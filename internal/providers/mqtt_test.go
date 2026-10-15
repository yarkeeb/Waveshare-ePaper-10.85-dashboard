package providers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
)

// Exercise the real MQTT client over TLS, without any broker/device credentials.
type testBroker struct {
	listener    net.Listener
	roots       *x509.CertPool
	mu          sync.Mutex
	connections []net.Conn
	wg          sync.WaitGroup
	handle      func(packets.ControlPacket, net.Conn) error
	errors      chan error
}

func newTestBroker(t *testing.T, handle func(packets.ControlPacket, net.Conn) error) *testBroker {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dashboard simulator"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	listener, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if e != nil {
		t.Fatal(e)
	}
	b := &testBroker{listener: listener, roots: roots, handle: handle, errors: make(chan error, 16)}
	b.wg.Go(func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			b.mu.Lock()
			b.connections = append(b.connections, conn)
			b.mu.Unlock()
			b.wg.Go(func() {
				defer conn.Close()
				for {
					p, e := packets.ReadPacket(conn)
					if e != nil {
						if !errors.Is(e, io.EOF) && !errors.Is(e, net.ErrClosed) {
							select {
							case b.errors <- e:
							default:
							}
						}
						return
					}
					if e = b.handle(p, conn); e != nil {
						b.errors <- e
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		listener.Close()
		b.mu.Lock()
		for _, c := range b.connections {
			c.Close()
		}
		b.mu.Unlock()
		b.wg.Wait()
		close(b.errors)
		for e := range b.errors {
			t.Error(e)
		}
	})
	return b
}
func (b *testBroker) client(opts *mqtt.ClientOptions) mqtt.Client {
	opts.SetTLSConfig(&tls.Config{RootCAs: b.roots, MinVersion: tls.VersionTLS12})
	return mqtt.NewClient(opts)
}
func ack(p packets.ControlPacket, c net.Conn) error {
	switch p := p.(type) {
	case *packets.ConnectPacket:
		return packets.NewControlPacket(packets.Connack).Write(c)
	case *packets.SubscribePacket:
		a := packets.NewControlPacket(packets.Suback).(*packets.SubackPacket)
		a.MessageID = p.MessageID
		a.ReturnCodes = []byte{0}
		return a.Write(c)
	case *packets.PingreqPacket:
		return packets.NewControlPacket(packets.Pingresp).Write(c)
	}
	return nil
}
func publish(c net.Conn, topic string, b []byte) error {
	p := packets.NewControlPacket(packets.Publish).(*packets.PublishPacket)
	p.TopicName = topic
	p.Payload = b
	return p.Write(c)
}
func TestBambuTLSReconnectAndFreshSnapshot(t *testing.T) {
	var mu sync.Mutex
	connects := 0
	broker := newTestBroker(t, func(p packets.ControlPacket, c net.Conn) error {
		switch p := p.(type) {
		case *packets.ConnectPacket:
			if p.Username != "bblp" || string(p.Password) != "access" {
				return errors.New("wrong printer credentials")
			}
			mu.Lock()
			connects++
			mu.Unlock()
		case *packets.SubscribePacket:
			if len(p.Topics) != 1 || p.Topics[0] != "device/serial/report" {
				return errors.New("wrong report topic")
			}
		case *packets.PublishPacket:
			if p.TopicName != "device/serial/request" || string(p.Payload) != `{"pushing":{"command":"pushall"}}` {
				return errors.New("unexpected printer command")
			}
			mu.Lock()
			n := connects
			mu.Unlock()
			if n == 1 {
				return publish(c, "device/serial/report", []byte(`{"print":{"gcode_state":"RUNNING","mc_percent":42,"layer_num":20,"total_layer_num":100}}`))
			}
			return publish(c, "device/serial/report", []byte(`{"print":{"gcode_state":"IDLE"}}`))
		}
		return ack(p, c)
	})
	cfg := config.Default()
	cfg.Bambu.Host = broker.listener.Addr().String()
	cfg.Bambu.Serial = "serial"
	cfg.Bambu.AccessCode = "access"
	b := NewBambu(cfg)
	b.newClient = broker.client
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, e := b.Poll(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if v.(model.Printer).Percent != 42 {
		t.Fatal(v)
	}
	b.mu.Lock()
	b.last = time.Now().Add(-3 * time.Minute)
	b.mu.Unlock()
	if _, e = b.Poll(ctx); e == nil {
		t.Fatal("stalled telemetry was accepted")
	}
	v, e = b.Poll(ctx)
	if e != nil {
		t.Fatal(e)
	}
	p := v.(model.Printer)
	if p.Status != "IDLE" || p.Percent != 0 || p.Layers != 0 {
		t.Fatal("old session telemetry leaked into new session", p)
	}
	mu.Lock()
	defer mu.Unlock()
	if connects != 2 {
		t.Fatal("stalled session was not reconnected", connects)
	}
}
func TestRoborockTLSRPCAndCleaningHistory(t *testing.T) {
	session := rrSession{IOT: rrIOT{U: "user", S: "secret", K: "key"}, Devices: []rrDevice{{DUID: "vacuum", LocalKey: "0123456789abcdef", PV: "1.0"}}}
	topic := "user/" + md5Hex("user:key")[2:10] + "/vacuum"
	broker := newTestBroker(t, func(p packets.ControlPacket, c net.Conn) error {
		switch p := p.(type) {
		case *packets.ConnectPacket:
			if p.Username != md5Hex("user:key")[2:10] || string(p.Password) != md5Hex("secret:key")[16:] {
				return errors.New("wrong vacuum credentials")
			}
		case *packets.SubscribePacket:
			if len(p.Topics) != 1 || p.Topics[0] != "rr/m/o/"+topic {
				return errors.New("wrong vacuum response topic")
			}
		case *packets.PublishPacket:
			if p.TopicName != "rr/m/i/"+topic {
				return errors.New("wrong vacuum command topic")
			}
			plain, e := rrDecode(session.Devices[0].LocalKey, p.Payload)
			if e != nil {
				return e
			}
			var env struct{ DPS map[string]string }
			if e = json.Unmarshal(plain, &env); e != nil {
				return e
			}
			var command struct {
				ID       uint32
				Method   string
				Security struct{ Endpoint, Nonce string }
			}
			if e = json.Unmarshal([]byte(env.DPS["101"]), &command); e != nil {
				return e
			}
			if command.Method != "get_status" || len(command.Security.Nonce) != 32 || command.Security.Endpoint == "" {
				return errors.New("missing status security envelope")
			}
			// Include an unrelated response in the same MQTT payload: correlation must skip it.
			var wire []byte
			for _, id := range []uint32{command.ID + 1, command.ID} {
				inner := fmt.Sprintf(`{"id":%d,"result":[{"state":8,"battery":95,"clean_area":24500000}]}`, id)
				body, _ := json.Marshal(map[string]any{"dps": map[string]string{"102": inner}})
				wire = append(wire, rrEncode(session.Devices[0].LocalKey, uint32(time.Now().Unix()), id, 102, body)...)
			}
			return publish(c, "rr/m/o/"+topic, wire)
		}
		return ack(p, c)
	})
	session.IOT.R.M = "ssl://" + broker.listener.Addr().String()
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	if e := writeJSON(filepath.Join(cfg.StateDir, "roborock_session.json"), session); e != nil {
		t.Fatal(e)
	}
	c := NewClient()
	c.newMQTT = broker.client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, e := c.Roborock(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	vac := v.(model.Vacuum)
	if vac.Battery != 95 || vac.Area != 24.5 || vac.Reference != 24.5 || vac.Last == "" || vac.Cleaning {
		t.Fatal(vac)
	}
	var saved map[string]any
	if e = readJSON(filepath.Join(cfg.StateDir, "roborock_stats.json"), &saved); e != nil {
		t.Fatal(e)
	}
	if number(saved["ref_area"]) != 24.5 {
		t.Fatal(saved)
	}
}

package providers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"net"
	"sync"
	"time"
)

func mqttWait(ctx context.Context, t mqtt.Token) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.Done():
		if t.Error() != nil {
			return errors.New("MQTT operation failed")
		}
		return nil
	}
}
func mqttOptions(broker, user, password string) *mqtt.ClientOptions {
	return mqtt.NewClientOptions().AddBroker(broker).SetUsername(user).SetPassword(password).SetClientID("dashboard-" + randomString(9)).SetConnectTimeout(10 * time.Second).SetWriteTimeout(10 * time.Second).SetKeepAlive(30 * time.Second).SetPingTimeout(10 * time.Second).SetAutoReconnect(false)
}

type Bambu struct {
	cfg        config.Config
	mu         sync.Mutex
	client     mqtt.Client
	data       map[string]any
	last       time.Time
	generation uint64
	now        func() time.Time
	newClient  func(*mqtt.ClientOptions) mqtt.Client
}

func NewBambu(cfg config.Config) *Bambu {
	return &Bambu{cfg: cfg, data: map[string]any{}, now: time.Now, newClient: mqtt.NewClient}
}
func (b *Bambu) Close() {
	if b.client != nil {
		b.client.Disconnect(100)
		b.client = nil
	}
}
func (b *Bambu) Poll(ctx context.Context) (any, error) {
	if b.client == nil || !b.client.IsConnected() {
		b.Close()
		b.mu.Lock()
		b.data = map[string]any{}
		b.last = time.Time{}
		b.generation++
		generation := b.generation
		b.mu.Unlock()
		host := b.cfg.Bambu.Host
		if _, _, e := net.SplitHostPort(host); e != nil {
			host = net.JoinHostPort(host, "8883")
		}
		opts := mqttOptions("ssl://"+host, "bblp", b.cfg.Bambu.AccessCode)
		// Bambu's LAN broker uses a self-signed certificate. The opt-in is explicit in config.
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: b.cfg.Bambu.InsecureTLS})
		b.client = b.newClient(opts)
		if e := mqttWait(ctx, b.client.Connect()); e != nil {
			b.Close()
			return nil, e
		}
		e := mqttWait(ctx, b.client.Subscribe("device/"+b.cfg.Bambu.Serial+"/report", 0, func(_ mqtt.Client, msg mqtt.Message) {
			var raw struct{ Print map[string]any }
			if json.Unmarshal(msg.Payload(), &raw) != nil || len(raw.Print) == 0 {
				return
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.generation != generation {
				return
			}
			updated := false
			for _, k := range []string{"gcode_state", "mc_percent", "mc_remaining_time", "layer_num", "total_layer_num"} {
				if v, ok := raw.Print[k]; ok && v != nil {
					b.data[k] = v
					updated = true
				}
			}
			if updated {
				b.last = b.now()
			}
		}))
		if e != nil {
			b.Close()
			return nil, e
		}
		if e = mqttWait(ctx, b.client.Publish("device/"+b.cfg.Bambu.Serial+"/request", 0, false, `{"pushing":{"command":"pushall"}}`)); e != nil {
			b.Close()
			return nil, e
		}
	}
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		b.mu.Lock()
		last := b.last
		p := model.Printer{Status: str(b.data, "gcode_state"), Percent: int(number(b.data["mc_percent"])), Remaining: int(number(b.data["mc_remaining_time"])), Layer: int(number(b.data["layer_num"])), Layers: int(number(b.data["total_layer_num"]))}
		b.mu.Unlock()
		if !b.client.IsConnected() {
			b.Close()
			return nil, errors.New("printer disconnected")
		}
		if !last.IsZero() && b.now().Sub(last) >= 2*time.Minute {
			b.Close()
			return nil, errors.New("printer telemetry stalled; reconnecting")
		}
		if !last.IsZero() && p.Status != "" {
			return p, nil
		}
		select {
		case <-ctx.Done():
			b.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			b.Close()
			return nil, errors.New("printer is offline or has no recent telemetry")
		case <-tick.C:
		}
	}
}

// Native Go adaptation of the V1 cloud protocol documented by python-roborock.
// Changes: read-only dashboard RPC client, Go AES framing and JSON credential storage.
// See third_party/python-roborock-LICENSE for Apache-2.0 license terms.
package providers

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"hash/crc32"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type rrIOT struct {
	U, S, H, K string
	R          struct{ A, M string }
}
type rrDevice struct {
	DUID     string `json:"duid"`
	LocalKey string `json:"localKey"`
	Name     string `json:"name"`
	PV       string `json:"pv"`
}
type rrSession struct {
	BaseURL, ClientID, Token string
	IOT                      rrIOT
	Devices                  []rrDevice
}

func md5Hex(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
func (c *Client) RoborockAuth(ctx context.Context, cfg config.Config, in io.Reader, out io.Writer) error {
	email := cfg.Roborock.Email
	if email == "" {
		return errors.New("set roborock.email before authentication")
	}
	var lookup struct {
		Code int
		Data struct{ URL string }
	}
	for _, region := range []string{"euiot", "usiot", "cniot", "ruiot"} {
		lookup.Code = 0
		lookup.Data.URL = ""
		e := c.Do(ctx, "POST", query("https://"+region+".roborock.com/api/v1/getUrlByEmail", url.Values{"email": {email}, "needtwostepauth": {"false"}}), nil, nil, &lookup)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e == nil && lookup.Code == 200 && lookup.Data.URL != "" {
			break
		}
		if lookup.Code == 2003 || lookup.Code == 9002 {
			break
		}
	}
	if lookup.Code != 200 || lookup.Data.URL == "" {
		return fmt.Errorf("Roborock region lookup failed (%d)", lookup.Code)
	}
	id := md5.Sum([]byte(email + randomString(16)))
	session := rrSession{BaseURL: lookup.Data.URL, ClientID: base64.StdEncoding.EncodeToString(id[:])}
	h := map[string]string{"header_clientid": session.ClientID}
	var reply struct {
		Code int
		Data struct {
			Token string
			IOT   rrIOT `json:"rriot"`
		}
	}
	if e := c.Do(ctx, "POST", query(session.BaseURL+"/api/v1/sendEmailCode", url.Values{"username": {email}, "type": {"auth"}}), h, nil, &reply); e != nil {
		return e
	}
	if reply.Code != 200 {
		return fmt.Errorf("Roborock code request failed (%d)", reply.Code)
	}
	fmt.Fprint(out, "Roborock email verification code: ")
	line, e := bufio.NewReader(in).ReadString('\n')
	if e != nil {
		return e
	}
	code := strings.TrimSpace(line)
	if code == "" {
		return errors.New("cancelled")
	}
	if e = c.Do(ctx, "POST", query(session.BaseURL+"/api/v1/loginWithCode", url.Values{"username": {email}, "verifycode": {code}, "verifycodetype": {"AUTH_EMAIL_CODE"}}), h, nil, &reply); e != nil {
		return e
	}
	if reply.Code != 200 || reply.Data.Token == "" {
		return fmt.Errorf("Roborock login failed (%d); check the code and app user agreement", reply.Code)
	}
	session.Token = reply.Data.Token
	session.IOT = reply.Data.IOT
	h["Authorization"] = session.Token
	var home struct {
		Code int
		Data struct {
			ID json.Number `json:"rrHomeId"`
		}
	}
	if e = c.Do(ctx, "GET", session.BaseURL+"/api/v1/getHomeDetail", h, nil, &home); e != nil {
		return e
	}
	if home.Code != 200 || home.Data.ID == "" {
		return errors.New("Roborock home discovery failed")
	}
	path := "/user/homes/" + home.Data.ID.String()
	var data struct {
		Success bool
		Result  struct {
			Devices  []rrDevice
			Received []rrDevice `json:"receivedDevices"`
		}
	}
	if e = c.Do(ctx, "GET", session.IOT.R.A+path, map[string]string{"Authorization": rrHawk(session.IOT, path, time.Now(), randomString(6))}, nil, &data); e != nil {
		return e
	}
	if !data.Success {
		return errors.New("Roborock device discovery failed")
	}
	session.Devices = append(data.Result.Devices, data.Result.Received...)
	if len(session.Devices) == 0 {
		return errors.New("no Roborock devices found")
	}
	for _, d := range session.Devices {
		fmt.Fprintf(out, "Device: %s (%s)\n", d.Name, d.DUID)
	}
	return writeJSON(filepath.Join(cfg.StateDir, "roborock_session.json"), session)
}
func rrHawk(r rrIOT, path string, now time.Time, nonce string) string {
	ts := fmt.Sprint(now.Unix())
	s := strings.Join([]string{r.U, r.S, nonce, ts, md5Hex(path), "", ""}, ":")
	mac := hmac.New(sha256.New, []byte(r.H))
	_, _ = mac.Write([]byte(s))
	return fmt.Sprintf(`Hawk id="%s",s="%s",ts="%s",nonce="%s",mac="%s"`, r.U, r.S, ts, nonce, base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}
func rrKey(key string, ts uint32) [16]byte {
	h := fmt.Sprintf("%08x", ts)
	var b strings.Builder
	for _, i := range []int{5, 6, 3, 7, 1, 2, 0, 4} {
		b.WriteByte(h[i])
	}
	return md5.Sum([]byte(b.String() + key + "TXdfu$jyZ#TZHsg4"))
}
func rrEncode(key string, ts, seq uint32, protocol uint16, payload []byte) []byte {
	k := rrKey(key, ts)
	block, _ := aes.NewCipher(k[:])
	n := 16 - len(payload)%16
	data := append([]byte(nil), payload...)
	for range n {
		data = append(data, byte(n))
	}
	for i := 0; i < len(data); i += 16 {
		block.Encrypt(data[i:i+16], data[i:i+16])
	}
	frame := make([]byte, 19+len(data)+4)
	copy(frame, "1.0")
	binary.BigEndian.PutUint32(frame[3:7], seq)
	binary.BigEndian.PutUint32(frame[7:11], seq^ts)
	binary.BigEndian.PutUint32(frame[11:15], ts)
	binary.BigEndian.PutUint16(frame[15:17], protocol)
	binary.BigEndian.PutUint16(frame[17:19], uint16(len(data)))
	copy(frame[19:], data)
	binary.BigEndian.PutUint32(frame[len(frame)-4:], crc32.ChecksumIEEE(frame[:len(frame)-4]))
	return frame
}
func rrDecode(key string, frame []byte) ([]byte, error) {
	if len(frame) >= 7 && string(frame[4:7]) == "1.0" {
		frame = frame[4:]
	}
	if len(frame) < 23 || string(frame[:3]) != "1.0" {
		return nil, errors.New("unsupported Roborock frame")
	}
	n := int(binary.BigEndian.Uint16(frame[17:19]))
	if n == 0 || n%16 != 0 || len(frame) != 19+n+4 {
		return nil, errors.New("invalid Roborock frame length")
	}
	if crc32.ChecksumIEEE(frame[:19+n]) != binary.BigEndian.Uint32(frame[19+n:]) {
		return nil, errors.New("Roborock checksum mismatch")
	}
	k := rrKey(key, binary.BigEndian.Uint32(frame[11:15]))
	block, _ := aes.NewCipher(k[:])
	data := append([]byte(nil), frame[19:19+n]...)
	for i := 0; i < n; i += 16 {
		block.Decrypt(data[i:i+16], data[i:i+16])
	}
	pad := int(data[n-1])
	if pad < 1 || pad > 16 || pad > n {
		return nil, errors.New("invalid Roborock padding")
	}
	for _, b := range data[n-pad:] {
		if int(b) != pad {
			return nil, errors.New("invalid Roborock padding")
		}
	}
	return data[:n-pad], nil
}
func (c *Client) Roborock(ctx context.Context, cfg config.Config) (any, error) {
	var s rrSession
	if e := readJSON(filepath.Join(cfg.StateDir, "roborock_session.json"), &s); e != nil {
		return nil, e
	}
	var d rrDevice
	for _, v := range s.Devices {
		if (cfg.Roborock.DeviceID == "" && (v.PV == "" || v.PV == "1.0")) || cfg.Roborock.DeviceID == v.DUID {
			d = v
			break
		}
	}
	if d.DUID == "" || d.LocalKey == "" {
		return nil, errors.New("Roborock device missing; authenticate again")
	}
	if d.PV != "" && d.PV != "1.0" {
		return nil, fmt.Errorf("Roborock protocol %s is not supported; upstream dashboard supports V1 vacuums", d.PV)
	}
	u, e := url.Parse(s.IOT.R.M)
	if e != nil || u.Host == "" || u.Scheme != "ssl" {
		return nil, errors.New("invalid Roborock TLS broker")
	}
	user := md5Hex(s.IOT.U + ":" + s.IOT.K)[2:10]
	password := md5Hex(s.IOT.S + ":" + s.IOT.K)[16:]
	client := c.mqtt(mqttOptions(u.String(), user, password))
	defer client.Disconnect(100)
	if e = mqttWait(ctx, client.Connect()); e != nil {
		return nil, e
	}
	responses := make(chan []byte, 4)
	topic := s.IOT.U + "/" + user + "/" + d.DUID
	if e = mqttWait(ctx, client.Subscribe("rr/m/o/"+topic, 0, func(_ mqtt.Client, m mqtt.Message) {
		frames, e := rrDecodeFrames(d.LocalKey, m.Payload())
		if e != nil {
			return
		}
		for _, b := range frames {
			select {
			case responses <- b:
			default:
			}
		}
	})); e != nil {
		return nil, e
	}
	id := uint32(time.Now().UnixMilli()%20000 + 10000)
	endpoint := md5.Sum([]byte(s.IOT.K))
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	inner, _ := json.Marshal(map[string]any{"id": id, "method": "get_status", "params": []any{}, "security": map[string]string{"endpoint": base64.StdEncoding.EncodeToString(endpoint[8:14]), "nonce": hex.EncodeToString(nonce)}})
	ts := uint32(time.Now().Unix())
	payload, _ := json.Marshal(map[string]any{"dps": map[string]string{"101": string(inner)}, "t": ts})
	if e = mqttWait(ctx, client.Publish("rr/m/i/"+topic, 0, false, rrEncode(d.LocalKey, ts, id, 101, payload))); e != nil {
		return nil, e
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errors.New("vacuum offline or response timed out")
		case b := <-responses:
			var envelope struct{ DPS map[string]json.RawMessage }
			if json.Unmarshal(b, &envelope) != nil {
				continue
			}
			var text string
			if json.Unmarshal(envelope.DPS["102"], &text) != nil {
				continue
			}
			var response struct {
				ID     uint32
				Error  json.RawMessage
				Result []struct {
					State   int
					Battery int
					Area    float64 `json:"clean_area"`
				}
			}
			if json.Unmarshal([]byte(text), &response) != nil || response.ID != id {
				continue
			}
			if len(response.Error) > 0 && string(response.Error) != "null" {
				return nil, errors.New("Roborock rejected status request")
			}
			if len(response.Result) == 0 {
				return nil, errors.New("Roborock status unavailable")
			}
			raw := response.Result[0]
			v := model.Vacuum{Battery: raw.Battery, Area: raw.Area / 1e6, Status: fmt.Sprintf("State %d", raw.State)}
			states := map[int]string{5: "Clean", 6: "Return", 8: "Charge", 10: "Pause", 17: "Spot", 18: "Room", 22: "Empty", 23: "Wash", 26: "To wash", 29: "Map"}
			if label, ok := states[raw.State]; ok {
				v.Status = label
				v.Cleaning = raw.State != 8
			}
			var old struct {
				Reference float64 `json:"ref_area"`
				Last      string  `json:"last_date"`
			}
			path := filepath.Join(cfg.StateDir, "roborock_stats.json")
			if e := readJSON(path, &old); e != nil && !errors.Is(e, os.ErrNotExist) {
				return nil, fmt.Errorf("read cleaning history: %w", e)
			}
			if !v.Cleaning && v.Area > 0 && v.Area != old.Reference {
				old.Reference = v.Area
				loc, _ := time.LoadLocation(cfg.Timezone)
				old.Last = time.Now().In(loc).Format("02 Jan 15:04")
				if e = writeJSON(path, old); e != nil {
					return nil, e
				}
			}
			v.Reference = old.Reference
			v.Last = old.Last
			if v.Cleaning && v.Reference > 0 {
				v.Percent = 100 * v.Area / v.Reference
			}
			return v, nil
		}
	}
}

// MQTT payloads may contain multiple frames, optionally with length prefixes.
func rrDecodeFrames(key string, data []byte) ([][]byte, error) {
	var result [][]byte
	for len(data) > 0 {
		if len(data) >= 7 && string(data[4:7]) == "1.0" {
			size := int(binary.BigEndian.Uint32(data[:4]))
			if size < 23 || size > len(data)-4 {
				return nil, errors.New("invalid Roborock frame prefix")
			}
			frame, e := rrDecode(key, data[4:4+size])
			if e != nil {
				return nil, e
			}
			result = append(result, frame)
			data = data[4+size:]
			continue
		}
		if len(data) < 19 {
			return nil, errors.New("truncated Roborock message")
		}
		size := 19 + int(binary.BigEndian.Uint16(data[17:19])) + 4
		if size > len(data) {
			return nil, errors.New("truncated Roborock frame")
		}
		frame, e := rrDecode(key, data[:size])
		if e != nil {
			return nil, e
		}
		result = append(result, frame)
		data = data[size:]
	}
	return result, nil
}

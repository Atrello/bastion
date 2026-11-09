package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Atrello/bastion/internal/panel/netdial"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
)

// Alerts: the panel watches what the agents report and raises an alert when a server goes offline, a
// container keeps restarting or is unhealthy, a disk is over 90%, or an image update is available. Each is
// sent once to every channel (ntfy, Discord, email), and again when it clears.

var alertKinds = []string{"offline", "crash", "unhealthy", "disk", "update", "host", "certificate"}

func (api *API) alertRoutes() {
	m := api.mux
	m.Handle("GET /api/alerts", api.owner(api.alertsPage))
	m.Handle("PUT /api/alerts/kinds", api.owner(api.setAlertKinds))
	m.Handle("POST /api/alerts/channels", api.owner(api.addChannel))
	m.Handle("DELETE /api/alerts/channels/{id}", api.owner(api.deleteChannel))
	m.Handle("POST /api/alerts/channels/{id}/test", api.owner(api.testChannel))
}

type channelConfig struct {
	// ntfy: the server's address and topic, and an access token if it needs one.
	URL   string `json:"url,omitempty"`
	Topic string `json:"topic,omitempty"`
	Token string `json:"token,omitempty"`
	// Discord: the webhook's address (in URL).
	// Email: an SMTP server, who it's from and to.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
}

type channel struct {
	ID     int64         `json:"id"`
	Kind   string        `json:"kind"`
	Name   string        `json:"name"`
	Detail string        `json:"detail"` // where it goes, without secrets
	config channelConfig // with them
}

func (api *API) channels(ctx context.Context) ([]channel, error) {
	rows, err := api.db.QueryContext(ctx, `SELECT id, kind, name, config FROM alert_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []channel{}
	for rows.Next() {
		var c channel
		var raw string
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &c.config)
		switch c.Kind {
		case "ntfy":
			c.Detail = strings.TrimRight(c.config.URL, "/") + "/" + c.config.Topic
		case "discord":
			c.Detail = "Discord webhook"
		case "email":
			c.Detail = c.config.To + " via " + c.config.Host
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (api *API) enabledKinds(ctx context.Context) []string {
	var raw string
	var kinds []string
	if api.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'alert_kinds'`).Scan(&raw) == nil {
		_ = json.Unmarshal([]byte(raw), &kinds)
	}
	return kinds
}

func (api *API) alertsPage(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	channels, err := api.channels(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	rows, err := api.db.QueryContext(r.Context(), `SELECT id, kind, title, detail, raised_at, cleared_at FROM alerts ORDER BY id DESC LIMIT 100`)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	type alert struct {
		ID        int64   `json:"id"`
		Kind      string  `json:"kind"`
		Title     string  `json:"title"`
		Detail    string  `json:"detail"`
		RaisedAt  string  `json:"raisedAt"`
		ClearedAt *string `json:"clearedAt"`
	}
	recent := []alert{}
	for rows.Next() {
		var a alert
		if rows.Scan(&a.ID, &a.Kind, &a.Title, &a.Detail, &a.RaisedAt, &a.ClearedAt) == nil {
			recent = append(recent, a)
		}
	}
	writeJSON(w, map[string]any{"channels": channels, "kinds": api.enabledKinds(r.Context()), "recent": recent})
}

func (api *API) setAlertKinds(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	var body struct{ Kinds []string }
	if !readJSON(w, r, &body) {
		return
	}
	kinds := []string{}
	for _, k := range body.Kinds {
		if slices.Contains(alertKinds, k) && !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	raw, _ := json.Marshal(kinds)
	if _, err := api.db.ExecContext(r.Context(), `UPDATE settings SET value = ? WHERE key = 'alert_kinds'`, string(raw)); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkChannel makes sure a channel's settings are complete and its addresses are web addresses.
func checkChannel(kind string, c channelConfig) error {
	webAddress := func(s string) bool {
		u, err := url.Parse(s)
		return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
	}
	switch kind {
	case "ntfy":
		if !webAddress(c.URL) || c.Topic == "" || strings.ContainsAny(c.Topic, "/?#") {
			return errors.New("give ntfy's address (like https://ntfy.sh) and a topic")
		}
	case "discord":
		if !webAddress(c.URL) || !strings.Contains(c.URL, "/api/webhooks/") {
			return errors.New("give the Discord channel's webhook address (Edit channel, Integrations, Webhooks)")
		}
	case "email":
		if c.Host == "" || c.Port <= 0 || c.Port > 65535 || !strings.Contains(c.From, "@") || !strings.Contains(c.To, "@") ||
			strings.ContainsAny(c.From+c.To, "\r\n") {
			return errors.New("give the SMTP server, its port, and who it's from and to")
		}
	default:
		return errors.New("a channel is ntfy, discord or email")
	}
	return nil
}

func (api *API) addChannel(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	var body struct {
		Kind, Name string
		Config     channelConfig
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := checkChannel(body.Kind, body.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 60 {
		name = body.Kind
	}
	raw, _ := json.Marshal(body.Config)
	if _, err := api.db.ExecContext(r.Context(), `INSERT INTO alert_channels (kind, name, config) VALUES (?, ?, ?)`, body.Kind, name, string(raw)); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteChannel(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM alert_channels WHERE id = ?`, id); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) testChannel(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := api.channels(r.Context())
	if err != nil {
		api.fail(w, r, err)
		return
	}
	for _, c := range list {
		if c.ID == id {
			if err := api.send(r.Context(), c, "Bastion test", "Alerts from "+api.cfg.PublicURL.Host+" will arrive here."); err != nil {
				writeError(w, http.StatusBadGateway, "it couldn't be sent: "+err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "no such channel")
}

var alertClient = &http.Client{Timeout: 15 * time.Second}

// send delivers one message to one channel.
func (api *API) send(ctx context.Context, c channel, title, body string) error {
	return api.sendLinked(ctx, c, title, body, "")
}

// sendLinked is send with a link the message is about (a backup's download, say): ntfy opens it when the
// notification is tapped; elsewhere it's in the message already.
func (api *API) sendLinked(ctx context.Context, c channel, title, body, link string) error {
	post := func(address string, payload []byte, headers map[string]string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := alertClient.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode >= 300 {
			return fmt.Errorf("it answered %s", res.Status)
		}
		return nil
	}
	switch c.Kind {
	case "ntfy":
		headers := map[string]string{"Title": title, "Tags": "bastion"}
		if link != "" {
			headers["Click"] = link
		}
		if c.config.Token != "" {
			headers["Authorization"] = "Bearer " + c.config.Token
		}
		return post(strings.TrimRight(c.config.URL, "/")+"/"+url.PathEscape(c.config.Topic), []byte(body), headers)
	case "discord":
		payload, _ := json.Marshal(map[string]any{"content": "**" + title + "**\n" + body, "allowed_mentions": map[string]any{"parse": []string{}}})
		return post(c.config.URL, payload, map[string]string{"Content-Type": "application/json"})
	case "email":
		oneLine := strings.NewReplacer("\r", " ", "\n", " ")
		message := "From: " + oneLine.Replace(c.config.From) + "\r\nTo: " + oneLine.Replace(c.config.To) +
			"\r\nSubject: " + mime.QEncoding.Encode("utf-8", oneLine.Replace(title)) +
			"\r\nDate: " + time.Now().Format(time.RFC1123Z) +
			"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
		return sendMail(c.config.Host, c.config.Port, c.config.Username, c.config.Password, c.config.From, c.config.To, []byte(message))
	}
	return errors.New("unknown channel")
}

// sendMail sends an email, all within 30 seconds so a stuck mail server can't hold up other alerts.
// Port 465 is TLS from the start; on any other port it upgrades with STARTTLS when the server offers it.
// A password is never sent without TLS, except to the panel's own host.
func sendMail(host string, port int, username, password, from, to string, message []byte) error {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := netdial.DialContext(dialCtx, "tcp", address)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if port == 465 {
		conn = tls.Client(conn, tlsConfig)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
	}
	if username != "" {
		// PlainAuth itself refuses to send the password without TLS (other than to localhost).
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// problem is something that should be raised as an alert while it lasts.
type problem struct{ key, kind, title, detail string }

// findProblems looks at every server's latest report for what's wrong now.
func (api *API) findProblems(ctx context.Context) ([]problem, error) {
	servers, err := api.hub.Servers(ctx)
	if err != nil {
		return nil, err
	}
	var found []problem
	for _, s := range servers {
		live := api.hub.Live(s.ID)
		if !live.Online {
			if s.LastSeen.Valid {
				if seen, err := time.Parse(time.RFC3339, s.LastSeen.String); err == nil && time.Since(seen) > 2*time.Minute {
					found = append(found, problem{fmt.Sprintf("offline:%d", s.ID), "offline", s.Name + " is offline", "Its agent hasn't been connected since " + seen.Format("15:04 on 2 Jan") + "."})
				}
			}
			continue
		}
		if m := live.Metrics; m != nil && m.DiskTotal > 0 && m.DiskUsed*100/m.DiskTotal >= 90 {
			found = append(found, problem{fmt.Sprintf("disk:%d", s.ID), "disk", s.Name + "'s disk is " + strconv.FormatUint(m.DiskUsed*100/m.DiskTotal, 10) + "% full", "Clean up unused images and volumes from its Images page."})
		}
		if live.State == nil {
			continue
		}
		for _, c := range live.State.Containers {
			switch {
			case c.State == "restarting":
				found = append(found, problem{fmt.Sprintf("crash:%d:%s", s.ID, c.Name), "crash", c.Name + " keeps restarting", "On " + s.Name + ": " + c.Status + ". Its logs will say why."})
			case strings.Contains(c.Status, "(unhealthy)"):
				found = append(found, problem{fmt.Sprintf("unhealthy:%d:%s", s.ID, c.Name), "unhealthy", c.Name + " is unhealthy", "On " + s.Name + ": its health check is failing."})
			}
		}
		for _, st := range live.State.Stacks {
			if st.Updates > 0 {
				found = append(found, problem{fmt.Sprintf("update:%d:%s", s.ID, st.Name), "update", "Updates for " + st.Name, fmt.Sprintf("On %s: %d image update(s) available. Update images on the stack's page.", s.Name, st.Updates)})
			}
		}
	}
	return append(found, api.npmProblems(ctx)...), nil
}

// watchAlerts checks for problems every 30 seconds, raising new ones and clearing those that have gone.
func (api *API) watchAlerts(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			api.checkAlerts(ctx)
			api.checkGameEvents(ctx)
		}
	}
}

func (api *API) checkAlerts(ctx context.Context) {
	found, err := api.findProblems(ctx)
	if err != nil {
		return
	}
	kinds := api.enabledKinds(ctx)
	open := map[string]int64{}
	rows, err := api.db.QueryContext(ctx, `SELECT id, key FROM alerts WHERE cleared_at IS NULL`)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		var key string
		if rows.Scan(&id, &key) == nil {
			open[key] = id
		}
	}
	rows.Close()
	channels, _ := api.channels(ctx)
	notify := func(title, detail string) {
		for _, c := range channels {
			if err := api.send(ctx, c, title, detail); err != nil {
				api.log.Warn("sending an alert", "channel", c.Name, "error", err)
			}
		}
	}

	now := map[string]bool{}
	for _, p := range found {
		now[p.key] = true
		if _, already := open[p.key]; already || !slices.Contains(kinds, p.kind) {
			continue
		}
		if _, err := api.db.ExecContext(ctx, `INSERT INTO alerts (key, kind, title, detail) VALUES (?, ?, ?, ?)`, p.key, p.kind, p.title, p.detail); err == nil {
			notify(p.title, p.detail)
		}
	}
	for key, id := range open {
		if now[key] {
			continue
		}
		var title string
		if api.db.QueryRowContext(ctx, `UPDATE alerts SET cleared_at = ? WHERE id = ? RETURNING title`, time.Now().UTC().Format(time.RFC3339), id).Scan(&title) == nil &&
			!strings.HasPrefix(key, "update:") {
			notify("Resolved: "+title, "It's back to normal.")
		}
	}
}

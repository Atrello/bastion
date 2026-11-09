package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Atrello/bastion/internal/panel/auth"
	"github.com/Atrello/bastion/internal/panel/eggs"
)

// Eggs: imported by hand (a file, pasted JSON or a link), checked, and replaced only when asked.

func (api *API) eggRoutes() {
	m := api.mux
	m.Handle("GET /api/eggs", api.owner(api.listEggs))
	m.Handle("GET /api/eggs/{id}", api.owner(api.getEgg))
	m.Handle("POST /api/eggs", api.owner(api.importEgg))
	m.Handle("DELETE /api/eggs/{id}", api.owner(api.deleteEgg))
}

const maxEgg = 1 << 20

func (api *API) listEggs(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rows, err := api.db.QueryContext(r.Context(), `SELECT e.id, e.name, e.category, e.author, e.description,
		(SELECT COUNT(*) FROM games g WHERE g.egg_id = e.id) FROM eggs e ORDER BY e.category, e.name`)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	defer rows.Close()
	type egg struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Category    string `json:"category"`
		Author      string `json:"author"`
		Description string `json:"description"`
		Servers     int    `json:"servers"`
	}
	list := []egg{}
	for rows.Next() {
		var e egg
		if rows.Scan(&e.ID, &e.Name, &e.Category, &e.Author, &e.Description, &e.Servers) == nil {
			list = append(list, e)
		}
	}
	writeJSON(w, list)
}

// loadEgg reads an egg from the database. Eggs were checked when imported; they're checked again here, as
// the checks may since have grown.
func (api *API) loadEgg(ctx context.Context, id int64) (*eggs.Egg, error) {
	var raw string
	if err := api.db.QueryRowContext(ctx, `SELECT egg FROM eggs WHERE id = ?`, id).Scan(&raw); err != nil {
		return nil, errors.New("no such egg")
	}
	return eggs.Parse([]byte(raw))
}

// getEgg is what the new game server form needs: its images and settings.
func (api *API) getEgg(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	e, err := api.loadEgg(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, map[string]any{"name": e.Name, "images": e.Images, "variables": e.Variables, "startup": e.Startup})
}

// importEgg takes an egg as JSON or from a link. An egg with the same name as one already here is only
// replaced when the request says so (the web app asks first); its game servers keep running with it.
func (api *API) importEgg(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		JSON     string `json:"json"`
		URL      string `json:"url"`
		Category string `json:"category"`
		Replace  bool   `json:"replace"`
	}
	if !readJSONLimit(w, r, &body, maxEgg+(64<<10)) {
		return
	}
	raw := []byte(body.JSON)
	if body.URL != "" {
		var err error
		if raw, err = fetchEgg(r.Context(), body.URL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	e, err := eggs.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	category := strings.TrimSpace(body.Category)
	if category == "" || len(category) > 40 {
		category = "Other"
	}
	var existing int64
	err = api.db.QueryRowContext(r.Context(), `SELECT id FROM eggs WHERE name = ?`, e.Name).Scan(&existing)
	switch {
	case err == nil && !body.Replace:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "there's already an egg called " + e.Name, "existing": existing})
		return
	case err == nil:
		_, err = api.db.ExecContext(r.Context(), `UPDATE eggs SET category = ?, author = ?, description = ?, egg = ?,
			imported_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, category, clipText(e.Author, 200), clipText(e.Description, 2000), string(raw), existing)
	case errors.Is(err, sql.ErrNoRows):
		_, err = api.db.ExecContext(r.Context(), `INSERT INTO eggs (name, category, author, description, egg) VALUES (?, ?, ?, ?, ?)`,
			e.Name, category, clipText(e.Author, 200), clipText(e.Description, 2000), string(raw))
	}
	if err != nil {
		api.fail(w, r, err)
		return
	}
	api.log.Info("egg imported", "user", u.Username, "egg", e.Name, "replaced", existing != 0)
	w.WriteHeader(http.StatusNoContent)
}

// fetchEgg downloads an egg from an https:// link, like a community repository's raw file.
func fetchEgg(ctx context.Context, link string) ([]byte, error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("the link should be an https:// address of the egg's JSON file")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) > 5 {
			return errors.New("the link redirects somewhere odd")
		}
		return nil
	}}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("the egg couldn't be downloaded")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("the link answered " + res.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxEgg+1))
	if err != nil || len(raw) > maxEgg {
		return nil, errors.New("that's too big to be an egg")
	}
	return raw, nil
}

func (api *API) deleteEgg(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var used int
	_ = api.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM games WHERE egg_id = ?`, id).Scan(&used)
	if used > 0 {
		writeError(w, http.StatusBadRequest, "game servers use this egg: delete them first")
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM eggs WHERE id = ?`, id); err != nil {
		api.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func clipText(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Atrello/bastion/internal/panel/auth"
)

// What a friend may do with a game shared with them. The owner chooses, per friend and game, from these;
// a friend always sees the game's state and figures. The owner can do everything.
var gamePermissions = []string{
	"console",   // read the console and send commands
	"power",     // start, stop, restart, kill
	"files",     // the file manager
	"backups",   // make, restore, download, delete
	"schedules", // make, change, run and delete schedules (each also needs what it does: see scheduleNeeds)
	"startup",   // change its startup settings
}

// permissions is what u may do with a game: everything for the owner, what its share says for a friend,
// nothing for anyone else.
func (api *API) permissions(ctx context.Context, u *auth.User, gameID int64) []string {
	if u.Role == auth.RoleOwner {
		return gamePermissions
	}
	var list *string
	if api.db.QueryRowContext(ctx, `SELECT permissions FROM game_shares WHERE game_id = ? AND user_id = ?`, gameID, u.ID).Scan(&list) != nil {
		return []string{}
	}
	if list == nil {
		return gamePermissions
	}
	return checkedPermissions(strings.Split(*list, ","))
}

// checkedPermissions keeps the ones that exist, in their usual order.
func checkedPermissions(asked []string) []string {
	kept := []string{}
	for _, p := range gamePermissions {
		if slices.Contains(asked, p) {
			kept = append(kept, p)
		}
	}
	return kept
}

// gameCan lets through whoever may do permission with the game: the owner, or a friend given it.
func (api *API) gameCan(permission string, next func(http.ResponseWriter, *http.Request, *auth.User, *game)) http.Handler {
	return api.gameUser(func(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
		if !slices.Contains(api.permissions(r.Context(), u, g.ID), permission) {
			writeError(w, http.StatusForbidden, "that part of this game server isn't shared with you")
			return
		}
		next(w, r, u, g)
	})
}

// scheduleNeeds is what a schedule's action needs besides "schedules", so a schedule can't do for a friend
// what they couldn't do by hand.
var scheduleNeeds = map[string]string{"restart": "power", "command": "console", "backup": "backups"}

// setSharePermissions changes what a friend may do with a game: {"permissions": ["console", "files"]}.
func (api *API) setSharePermissions(w http.ResponseWriter, r *http.Request, u *auth.User, g *game) {
	userID, _ := strconv.ParseInt(r.PathValue("user"), 10, 64)
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	kept := checkedPermissions(body.Permissions)
	res, err := api.db.ExecContext(r.Context(), `UPDATE game_shares SET permissions = ? WHERE game_id = ? AND user_id = ?`, strings.Join(kept, ","), g.ID, userID)
	if err != nil {
		api.fail(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "it isn't shared with them")
		return
	}
	api.log.Info("game share changed", "user", u.Username, "game", g.ID, "friend", userID, "can", strings.Join(kept, ", "))
	w.WriteHeader(http.StatusNoContent)
}

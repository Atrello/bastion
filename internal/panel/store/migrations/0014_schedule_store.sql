-- Where a game's backup schedule puts its backups. Neither set: wherever the game's settings say
-- (games.backup_store_id). on_server: on the game's own server. store_id: that backup storage; if it's
-- removed, the schedule goes back to the game's setting.
ALTER TABLE game_schedules ADD COLUMN store_id INTEGER REFERENCES backup_stores (id) ON DELETE SET NULL;
ALTER TABLE game_schedules ADD COLUMN on_server INTEGER NOT NULL DEFAULT 0;

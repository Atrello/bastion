-- What a friend may do with a game shared with them: a comma-separated list of console, power, files,
-- backups, schedules and startup. NULL is everything, as shares were before there was a choice.
ALTER TABLE game_shares ADD COLUMN permissions TEXT;

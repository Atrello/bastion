-- An NPM with a self-signed certificate is linked by trusting that one certificate: its SHA-256 is kept
-- here, and only it is accepted from then on. Empty for a certificate trusted the usual way, or plain http.
ALTER TABLE npm_instances ADD COLUMN cert_sha256 TEXT NOT NULL DEFAULT '';

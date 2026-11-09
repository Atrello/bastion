-- Which NPM it is: npm (Nginx Proxy Manager) or npmplus (a fork with its own fields), found when it's
-- linked. The web app's forms send each the fields it takes.
ALTER TABLE npm_instances ADD COLUMN flavour TEXT NOT NULL DEFAULT 'npm';

FROM caddy:2.10-alpine

WORKDIR /srv

COPY index.html app.js styles.css config.js /srv/
COPY components /srv/components
COPY lib /srv/lib
COPY Caddyfile /etc/caddy/Caddyfile
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

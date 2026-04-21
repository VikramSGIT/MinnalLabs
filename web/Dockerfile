FROM node:22-alpine AS builder

WORKDIR /build
COPY . .

RUN apk add --no-cache brotli && \
    mkdir -p dist && \
    npx --yes esbuild app.js \
      --bundle --minify --tree-shaking=true \
      --target=es2022 --format=esm \
      --outfile=dist/app.min.js && \
    npx esbuild styles.css \
      --bundle --minify \
      --outfile=dist/styles.min.css && \
    { \
      printf '<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>IoT Enrollment</title><style>'; \
      cat dist/styles.min.css; \
      printf '</style></head><body><app-shell></app-shell><noscript>This app requires JavaScript to enroll devices.</noscript><script src="./config.js"></script><script type="module">'; \
      cat dist/app.min.js; \
      printf '</script></body></html>'; \
    } > dist/index.html && \
    rm dist/app.min.js dist/styles.min.css && \
    brotli --best dist/index.html && \
    gzip -9 -k dist/index.html

FROM caddy:2.10-alpine

WORKDIR /srv

COPY --from=builder /build/dist /srv
COPY privacy-policy.html /srv/privacy-policy.html
COPY Caddyfile /etc/caddy/Caddyfile
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

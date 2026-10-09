# The service's SQL engine, Postgres 18. This FROM line is Postgres's one pin.
FROM postgres:18.6-alpine

# The superuser, its password, and the database the image creates on first
# start, paired with config.json's database block and secrets.example.json.
ENV POSTGRES_USER=app \
    POSTGRES_PASSWORD=app \
    POSTGRES_DB=app

# Healthy once the server accepts connections on TCP. -h 127.0.0.1 keeps the
# probe off the Unix socket, where the entrypoint's first-start init server
# (which listens on no TCP address) would answer before the real server runs.
HEALTHCHECK --start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5 \
    CMD ["pg_isready", "-h", "127.0.0.1", "-U", "app", "-d", "app"]

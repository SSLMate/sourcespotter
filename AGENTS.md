## Testing Procedure

Ensure that `go test` reports no failures.

```
go test ./...
```

You can start the sourcespotter web interface for testing by running:

```
./cmd/sourcespotter/sourcespotter -config testenv/config.json
```

Once sourcespotter has been started with this config, the web interface can be accessed at `https://sourcespotter.localhost:8443` and API endpoints can be accessed at `https://APINAME.api.sourcespotter.localhost:8443`. For example, the gossip endpoint is at `https://gossip.api.sourcespotter.localhost:8443`.

If you make any changes to the web interface or dashboards, you should always test it by starting sourcespotter and connecting to it to make sure your changes look right. If you have access to a headless web browser, use it. Otherwise, use curl.

You can connect to the PostgreSQL database on localhost. Database, username, and password are `sourcespotter`.

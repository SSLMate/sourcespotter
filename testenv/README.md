# Source Spotter Test Environment

This directory contains a helper script that spins up a local instance of
Source Spotter for development and testing.

## Set Up

Run the `setup.sh` script as root:

```sh
sudo ./setup.sh
```

The script installs PostgreSQL and mkcert, creates a database with a small
amount of test data, and generates TLS certificates for `sourcespotter.localhost`.

## Running

To launch Source Spotter in the test environment, run:

```sh
./run.sh
```

Once running, visit <https://sourcespotter.localhost:8443> in your browser.
You may need to add an entry to `/etc/hosts` pointing `sourcespotter.localhost`
and `gossip.api.sourcespotter.localhost` to `127.0.0.1`.

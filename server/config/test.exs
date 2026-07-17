import Config

database_url =
  "ecto://autoboard:autoboard@localhost/autoboard_test#{System.get_env("MIX_TEST_PARTITION")}"

data_dir = Path.expand("../var", __DIR__)

config :autoboard,
  database_url: database_url,
  data_dir: data_dir,
  http_ip: {127, 0, 0, 1},
  http_port: 0,
  socket_path: Path.join(data_dir, "autoboard.sock"),
  max_attachment_bytes: 52_428_800

config :autoboard, Autoboard.Repo,
  url: database_url,
  pool: Ecto.Adapters.SQL.Sandbox

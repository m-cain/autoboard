import Config

data_dir = System.get_env("AUTOBOARD_DATA_DIR") || Path.expand("var", File.cwd!())
socket_path = System.get_env("AUTOBOARD_SOCKET") || Path.join(data_dir, "autoboard.sock")

database_url =
  System.get_env("DATABASE_URL") ||
    "ecto://autoboard:autoboard@localhost/autoboard_dev"

config :autoboard,
  ecto_repos: [Autoboard.Repo],
  database_url: database_url,
  data_dir: data_dir,
  http_ip: {127, 0, 0, 1},
  http_port: 4040,
  max_attachment_bytes: 52_428_800,
  socket_path: socket_path

config :autoboard, Autoboard.Repo, url: database_url

import_config "#{config_env()}.exs"

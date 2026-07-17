import Config

if config_env() != :test do
  database_url =
    System.get_env("DATABASE_URL") ||
      "ecto://autoboard:autoboard@localhost/autoboard_dev"

  # Keep the default stable between Mix and a release. Both documented commands
  # run from `server`, while `__DIR__` points inside a relocated release.
  data_dir = System.get_env("AUTOBOARD_DATA_DIR") || Path.expand("var", File.cwd!())

  http_port =
    System.get_env("AUTOBOARD_HTTP_PORT", "4040")
    |> String.to_integer()

  config :autoboard,
    database_url: database_url,
    data_dir: data_dir,
    http_ip: {127, 0, 0, 1},
    http_port: http_port,
    socket_path: System.get_env("AUTOBOARD_SOCKET") || Path.join(data_dir, "autoboard.sock")

  config :autoboard, Autoboard.Repo, url: database_url
end

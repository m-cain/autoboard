import Config

if config_env() != :test do
  database_url =
    System.get_env("DATABASE_URL") ||
      "ecto://autoboard:autoboard@localhost/autoboard_dev"

  config :autoboard,
    database_url: database_url

  config :autoboard, Autoboard.Repo, url: database_url
end

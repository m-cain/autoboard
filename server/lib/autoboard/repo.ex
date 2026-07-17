defmodule Autoboard.Repo do
  use Ecto.Repo,
    otp_app: :autoboard,
    adapter: Ecto.Adapters.Postgres
end

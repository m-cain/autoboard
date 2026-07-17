defmodule Autoboard.Application do
  # See https://hexdocs.pm/elixir/Application.html
  # for more information on OTP Applications
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Autoboard.Repo,
      {Registry, keys: :duplicate, name: Autoboard.Activity.Registry},
      Autoboard.Attachments.Cleanup,
      {Autoboard.RPC.Listener, name: Autoboard.RPC.Listener}
    ]

    # See https://hexdocs.pm/elixir/Supervisor.html
    # for other strategies and supported options
    opts = [strategy: :one_for_one, name: Autoboard.Supervisor]
    Supervisor.start_link(children, opts)
  end
end

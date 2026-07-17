defmodule Autoboard.Release do
  @moduledoc """
  Release-time database operations which do not depend on Mix being available.
  """

  @spec migrate() :: :ok
  def migrate do
    Application.load(:autoboard)

    for repo <- Application.fetch_env!(:autoboard, :ecto_repos) do
      {:ok, _, _} =
        Ecto.Migrator.with_repo(repo, fn started_repo ->
          Ecto.Migrator.run(started_repo, :up, all: true)
        end)
    end

    :ok
  end
end

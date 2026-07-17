defmodule Mix.Tasks.Autoboard.Setup do
  @moduledoc """
  Creates or migrates Autoboard storage and prepares private attachment directories.
  """

  use Mix.Task

  alias Autoboard.Attachments.Storage
  alias Autoboard.Release

  @shortdoc "Migrates Autoboard and creates managed data directories"

  @impl Mix.Task
  def run([]) do
    Release.migrate()
    ensure_data_dir()
    :ok = Storage.ensure_managed_dirs()
    :ok
  end

  def run(_args), do: Mix.raise("mix autoboard.setup does not accept arguments")

  defp ensure_data_dir do
    data_dir = Application.fetch_env!(:autoboard, :data_dir)
    :ok = File.mkdir_p(data_dir)
    :ok = File.chmod(data_dir, 0o700)
  end
end

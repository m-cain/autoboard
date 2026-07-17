defmodule Mix.Tasks.Autoboard.Token.Create do
  @moduledoc """
  Issues one global access token and prints its plaintext once.
  """

  use Mix.Task

  alias Autoboard.Auth.Token

  @shortdoc "Creates an Autoboard token for --actor me|codex"

  @impl Mix.Task
  def run(args) do
    actor = parse_actor!(args)
    ensure_repo_started()

    case Token.issue(actor) do
      {:ok, plaintext, _token} ->
        IO.binwrite(:stdio, plaintext <> "\n")
        :ok

      {:error, error} ->
        Mix.raise("could not create token: #{error.message}")
    end
  end

  defp ensure_repo_started do
    case Process.whereis(Autoboard.Repo) do
      nil ->
        {:ok, _started} = Application.ensure_all_started(:ecto_sql)
        {:ok, _repo} = Autoboard.Repo.start_link()
        :ok

      _repo ->
        :ok
    end
  end

  defp parse_actor!(args) do
    {options, rest, invalid} = OptionParser.parse(args, strict: [actor: :string])

    actor_options = Enum.count(args, &(&1 == "--actor" or String.starts_with?(&1, "--actor=")))

    if rest != [] or invalid != [] or actor_options > 1 do
      Mix.raise("mix autoboard.token.create accepts only --actor me|codex")
    end

    case Keyword.fetch(options, :actor) do
      {:ok, "me"} -> :me
      {:ok, "codex"} -> :codex
      {:ok, _actor} -> Mix.raise("--actor must be me or codex")
      :error -> Mix.raise("mix autoboard.token.create requires --actor me or codex")
    end
  end
end

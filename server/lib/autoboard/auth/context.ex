defmodule Autoboard.Auth.Context do
  @enforce_keys [:actor, :scope]
  defstruct [:actor, :scope]

  @type actor :: :me | :codex | :system
  @type scope :: :global | {:project, Ecto.UUID.t()}
  @type t :: %__MODULE__{actor: actor(), scope: scope()}

  def global(actor) when actor in [:me, :codex], do: %__MODULE__{actor: actor, scope: :global}

  def project(actor, project_id) when actor in [:me, :codex] do
    case Ecto.UUID.cast(project_id) do
      {:ok, project_id} -> %__MODULE__{actor: actor, scope: {:project, project_id}}
      :error -> raise ArgumentError, "project_id must be a valid UUID"
    end
  end
end

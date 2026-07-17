defmodule Autoboard.Auth.Context do
  @enforce_keys [:actor, :scope]
  defstruct [:actor, :scope]

  @type actor :: :me | :codex | :system
  @type t :: %__MODULE__{actor: actor(), scope: :global}

  def global(actor) when actor in [:me, :codex], do: %__MODULE__{actor: actor, scope: :global}
end

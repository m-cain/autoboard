defmodule Autoboard.Domain.Error do
  @enforce_keys [:kind, :message]
  defstruct [:kind, :message, fields: %{}, current: nil]

  @type t :: %__MODULE__{kind: atom(), message: String.t(), fields: map(), current: term()}
end

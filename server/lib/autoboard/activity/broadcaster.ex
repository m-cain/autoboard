defmodule Autoboard.Activity.Broadcaster do
  @registry Autoboard.Activity.Registry

  @spec broadcast(term()) :: :ok
  def broadcast(event) do
    Registry.dispatch(@registry, :activity, fn entries ->
      for {pid, _value} <- entries, do: send(pid, {:activity, event})
    end)

    :ok
  end
end

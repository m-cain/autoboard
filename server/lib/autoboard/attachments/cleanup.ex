defmodule Autoboard.Attachments.Cleanup do
  use GenServer

  alias Autoboard.Attachments

  def start_link(_opts), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    send(self(), :cleanup)
    {:ok, nil}
  end

  @impl true
  def handle_info(:cleanup, state) do
    Attachments.cleanup()
    {:noreply, state}
  end
end

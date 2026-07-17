defmodule Autoboard.RPC.ListenerTest do
  use ExUnit.Case, async: false

  alias Autoboard.RPC.Listener

  test "refuses a regular file at the configured socket path without deleting it" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-file-#{System.unique_integer([:positive])}.sock"
      )

    :ok = File.write(path, "must survive")
    previous_trap_exit = Process.flag(:trap_exit, true)

    on_exit(fn ->
      Process.flag(:trap_exit, previous_trap_exit)

      case File.lstat(path) do
        {:ok, %{type: :regular}} -> File.rm(path)
        _ -> :ok
      end
    end)

    assert {:error, :unsafe_existing_socket_path} = Listener.start_link(path: path)
    assert {:ok, "must survive"} = File.read(path)
  end

  test "restarts a killed acceptor and accepts a new connection" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-restart-#{System.unique_integer([:positive])}.sock"
      )

    {:ok, listener} = start_supervised({Listener, path: path})
    state = :sys.get_state(listener)
    Process.exit(state.acceptor, :kill)

    assert eventually(fn -> :sys.get_state(listener).acceptor != state.acceptor end)
    assert {:ok, socket} = Autoboard.RPCClient.connect(path)
    :ok = :gen_tcp.close(socket)
  end

  defp eventually(fun, attempts \\ 20)
  defp eventually(fun, 0), do: fun.()

  defp eventually(fun, attempts) do
    if fun.(),
      do: true,
      else:
        (
          Process.sleep(10)
          eventually(fun, attempts - 1)
        )
  end
end

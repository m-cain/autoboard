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
end

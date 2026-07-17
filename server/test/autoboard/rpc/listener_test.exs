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

    assert {:error, :ambiguous_socket_path} = Listener.start_link(path: path)
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

  test "never unlinks a live listener or a replacement endpoint" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-owner-#{System.unique_integer([:positive])}.sock"
      )

    previous_trap_exit = Process.flag(:trap_exit, true)
    on_exit(fn -> Process.flag(:trap_exit, previous_trap_exit) end)

    {:ok, first} = Listener.start_link(path: path)
    assert {:error, :socket_in_use} = Listener.start_link(path: path)
    assert {:ok, socket} = Autoboard.RPCClient.connect(path)
    :ok = :gen_tcp.close(socket)

    :ok = File.rm(path)
    assert {:error, :socket_in_use} = Listener.start_link(path: path)
    :ok = GenServer.stop(first)
    refute File.exists?(path)
  end

  test "replaces a proven stale current-user socket" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-stale-#{System.unique_integer([:positive])}.sock"
      )

    {:ok, stale} = :gen_tcp.listen(0, [:binary, ifaddr: {:local, String.to_charlist(path)}])
    :ok = :gen_tcp.close(stale)
    assert File.exists?(path)
    {:ok, stat} = File.lstat(path)

    identity = [
      stat.major_device,
      stat.minor_device,
      stat.inode,
      Bitwise.band(stat.mode, 0o170000),
      stat.uid
    ]

    :ok =
      File.write(
        path <> ".owner",
        Jason.encode!(%{
          "pid" => "999999",
          "nonce" => Ecto.UUID.generate(),
          "identity" => identity
        })
      )

    :ok = File.chmod(path <> ".owner", 0o600)
    {:ok, listener} = start_supervised({Listener, path: path})
    assert {:ok, socket} = Autoboard.RPCClient.connect(path)
    :ok = :gen_tcp.close(socket)
    :ok = GenServer.stop(listener)
  end

  test "cleans a socket bound before an injected chmod failure" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-partial-#{System.unique_integer([:positive])}.sock"
      )

    previous_trap_exit = Process.flag(:trap_exit, true)
    on_exit(fn -> Process.flag(:trap_exit, previous_trap_exit) end)

    assert {:error, {:injected_failure, :chmod}} =
             Listener.start_link(path: path, fail_stage: :chmod)

    refute File.exists?(path)
  end

  test "cleans post-bind supervisor and acceptor failures and permits rebinding" do
    previous_trap_exit = Process.flag(:trap_exit, true)
    on_exit(fn -> Process.flag(:trap_exit, previous_trap_exit) end)

    for stage <- [:supervisor, :acceptor] do
      path =
        Path.join(
          System.tmp_dir!(),
          "autoboard-rpc-partial-#{stage}-#{System.unique_integer([:positive])}.sock"
        )

      assert {:error, {:injected_failure, ^stage}} =
               Listener.start_link(path: path, fail_stage: stage)

      refute File.exists?(path)
      {:ok, listener} = Listener.start_link(path: path)
      assert File.exists?(path)
      :ok = GenServer.stop(listener)
      refute File.exists?(path)
    end
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

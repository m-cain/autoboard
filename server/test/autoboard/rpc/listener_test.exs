defmodule Autoboard.RPC.ListenerTest do
  use ExUnit.Case, async: false

  alias Autoboard.RPC.Listener

  test "refuses a regular file at the configured socket path without deleting it" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-file-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
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
    refute File.exists?(path <> ".owner")
    refute File.exists?(path <> ".owner.claim")
  end

  test "removes provisional ownership after a forced pre-bind failure" do
    path = socket_path("pre-bind")
    previous_trap_exit = Process.flag(:trap_exit, true)
    on_exit(fn -> Process.flag(:trap_exit, previous_trap_exit) end)

    for stage <- [:listen, :identity, :owner_write] do
      assert {:error, {:injected_failure, ^stage}} =
               Listener.start_link(path: path, fail_stage: stage)

      refute File.exists?(path)
      refute File.exists?(path <> ".owner")
      refute File.exists?(path <> ".owner.claim")
    end
  end

  test "leaves untrusted owner markers untouched" do
    for {name, write_marker} <- [
          {:world_readable, fn path -> write_marker(path, marker_record(), 0o644) end},
          {:extra_key,
           fn path -> write_marker(path, Map.put(marker_record(), "extra", true), 0o600) end},
          {:oversized, fn path -> File.write(path <> ".owner", String.duplicate("x", 2_048)) end}
        ] do
      path = socket_path("untrusted-#{name}")
      :ok = write_marker.(path)
      previous_trap_exit = Process.flag(:trap_exit, true)

      on_exit(fn ->
        Process.flag(:trap_exit, previous_trap_exit)
        File.rm(path <> ".owner")
      end)

      assert {:error, :ambiguous_socket_ownership} = Listener.start_link(path: path)
      assert File.exists?(path <> ".owner")
      refute File.exists?(path <> ".owner.claim")
    end
  end

  test "allows exactly one of eight simultaneous stale-owner contenders to serve" do
    path = socket_path("concurrent-stale")
    write_trusted_stale_marker(path)
    parent = self()

    contenders =
      for _ <- 1..8 do
        spawn(fn ->
          Process.flag(:trap_exit, true)
          result = Listener.start_link(path: path)
          send(parent, {:contender_result, self(), result})

          receive do
            :stop ->
              if match?({:ok, _}, result), do: GenServer.stop(elem(result, 1))
          end
        end)
      end

    on_exit(fn ->
      Enum.each(contenders, &send(&1, :stop))
      File.rm(path)
      File.rm(path <> ".owner")
      File.rmdir(path <> ".owner.claim")
    end)

    results =
      for _ <- 1..8 do
        receive do
          {:contender_result, _pid, result} -> result
        after
          2_000 -> :timeout
        end
      end

    winners = Enum.filter(results, &match?({:ok, _}, &1))

    assert [{:ok, _listener}] = winners

    assert Enum.all?(
             results -- winners,
             &(&1 in [{:error, :socket_in_use}, {:error, :socket_ownership_contended}])
           )

    assert {:ok, socket} = Autoboard.RPCClient.connect(path)
    :ok = :gen_tcp.close(socket)
    assert {:ok, _} = File.read(path <> ".owner")
  end

  test "rejects a symlinked owner marker without following it" do
    path = socket_path("symlink-marker")
    target = path <> ".marker-target"
    :ok = File.write(target, Jason.encode!(marker_record()))
    :ok = File.chmod(target, 0o600)
    :ok = File.ln_s(target, path <> ".owner")
    previous_trap_exit = Process.flag(:trap_exit, true)

    on_exit(fn ->
      Process.flag(:trap_exit, previous_trap_exit)
      File.rm(path <> ".owner")
      File.rm(target)
    end)

    assert {:error, :ambiguous_socket_ownership} = Listener.start_link(path: path)
    assert {:ok, %{type: :symlink}} = File.lstat(path <> ".owner")
  end

  test "preserves an unowned live socket endpoint" do
    path = socket_path("live-backlog")
    {:ok, socket} = :gen_tcp.listen(0, [:binary, ifaddr: {:local, String.to_charlist(path)}])
    previous_trap_exit = Process.flag(:trap_exit, true)

    on_exit(fn ->
      Process.flag(:trap_exit, previous_trap_exit)
      :gen_tcp.close(socket)
      File.rm(path)
    end)

    assert {:error, :ambiguous_socket_path} = Listener.start_link(path: path)
    assert File.exists?(path)
  end

  test "does not remove a valid replacement endpoint or marker during old listener shutdown" do
    path = socket_path("replacement")
    previous_trap_exit = Process.flag(:trap_exit, true)
    {:ok, old_listener} = Listener.start_link(path: path)
    :ok = File.rm(path)
    {:ok, replacement} = :gen_tcp.listen(0, [:binary, ifaddr: {:local, String.to_charlist(path)}])
    {:ok, stat} = File.lstat(path)

    :ok =
      write_marker(
        path,
        %{
          "version" => 1,
          "pid" => :os.getpid() |> List.to_string(),
          "nonce" => Ecto.UUID.generate(),
          "identity" => [
            stat.major_device,
            stat.minor_device,
            stat.inode,
            Bitwise.band(stat.mode, 0o170000),
            stat.uid
          ]
        },
        0o600
      )

    on_exit(fn ->
      Process.flag(:trap_exit, previous_trap_exit)
      :gen_tcp.close(replacement)
      File.rm(path)
      File.rm(path <> ".owner")
      File.rmdir(path <> ".owner.claim")
    end)

    :ok = GenServer.stop(old_listener)
    assert File.exists?(path)
    assert File.exists?(path <> ".owner")
    refute File.exists?(path <> ".owner.claim")
  end

  test "restarts a killed acceptor and accepts a new connection" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-restart-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
      )

    {:ok, listener} = start_supervised({Listener, path: path})
    state = :sys.get_state(listener)
    Process.exit(state.acceptor, :kill)

    assert eventually(fn -> :sys.get_state(listener).acceptor != state.acceptor end)
    assert {:ok, socket} = Autoboard.RPCClient.connect(path)
    :ok = :gen_tcp.close(socket)
  end

  test "removes its owned endpoint when its supervisor shuts down" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-supervisor-stop-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
      )

    {:ok, supervisor} = Supervisor.start_link([{Listener, path: path}], strategy: :one_for_one)
    on_exit(fn -> if Process.alive?(supervisor), do: Supervisor.stop(supervisor) end)

    assert File.exists?(path)
    assert File.exists?(path <> ".owner")
    assert :ok = Supervisor.stop(supervisor)
    refute File.exists?(path)
    refute File.exists?(path <> ".owner")
  end

  test "never unlinks a live listener or a replacement endpoint" do
    path =
      Path.join(
        System.tmp_dir!(),
        "autoboard-rpc-owner-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
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
        "autoboard-rpc-stale-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
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
          "version" => 1,
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
        "autoboard-rpc-partial-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
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
          "autoboard-rpc-partial-#{stage}-#{Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)}.sock"
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

  defp socket_path(label) do
    suffix = Base.encode16(:crypto.strong_rand_bytes(4), case: :lower)
    Path.join(System.tmp_dir!(), "autoboard-rpc-#{label}-#{suffix}.sock")
  end

  defp marker_record do
    %{
      "version" => 1,
      "pid" => "999999",
      "nonce" => Ecto.UUID.generate(),
      "identity" => [1, 0, 1, 0o140000, 501]
    }
  end

  defp write_marker(path, record, mode) do
    :ok = File.write(path <> ".owner", Jason.encode!(record))
    File.chmod(path <> ".owner", mode)
  end

  defp write_trusted_stale_marker(path) do
    {:ok, socket} = :gen_tcp.listen(0, [:binary, ifaddr: {:local, String.to_charlist(path)}])
    :ok = :gen_tcp.close(socket)
    {:ok, stat} = File.lstat(path)

    write_marker(
      path,
      %{
        "version" => 1,
        "pid" => "999999",
        "nonce" => Ecto.UUID.generate(),
        "identity" => [
          stat.major_device,
          stat.minor_device,
          stat.inode,
          Bitwise.band(stat.mode, 0o170000),
          stat.uid
        ]
      },
      0o600
    )
  end
end

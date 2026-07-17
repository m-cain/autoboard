defmodule Autoboard.RPC.Listener do
  @moduledoc false
  use GenServer
  alias Autoboard.RPC.Acceptor
  @max_frame_bytes 4_194_304
  @socket_file_type 0o140000
  @file_type_mask 0o170000
  @marker_mode 0o600
  @claim_mode 0o700
  @marker_version 1
  @max_marker_bytes 1024
  @max_identity_value 9_223_372_036_854_775_807

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, opts, Keyword.take(opts, [:name]))

  @impl true
  def init(opts) do
    Process.flag(:trap_exit, true)
    parent = self() |> Process.info(:links) |> elem(1) |> List.first()
    path = Keyword.get(opts, :path, Application.fetch_env!(:autoboard, :socket_path))

    with :ok <- File.mkdir_p(Path.dirname(path)), {:ok, owner} <- acquire_owner(path) do
      case bind_listener(path, owner, opts) do
        {:ok, state} ->
          {:ok, Map.put(state, :parent, parent)}

        {:error, reason} ->
          cleanup_owner(path, owner)
          {:stop, reason}
      end
    else
      {:error, reason} -> {:stop, reason}
    end
  end

  @impl true
  def terminate(_reason, state) do
    :gen_tcp.close(state.socket)
    Process.exit(state.session_supervisor, :shutdown)

    cleanup_owner(state.path, state.owner, state.identity)

    :ok
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{acceptor_ref: ref} = state) do
    case Task.Supervisor.start_child(state.session_supervisor, fn ->
           Acceptor.accept_loop(state.socket, state.session_supervisor)
         end) do
      {:ok, acceptor} ->
        {:noreply, %{state | acceptor: acceptor, acceptor_ref: Process.monitor(acceptor)}}

      {:error, _} ->
        {:stop, :acceptor_unavailable, state}
    end
  end

  @impl true
  def handle_info({:EXIT, parent, reason}, %{parent: parent} = state),
    do: {:stop, reason, state}

  def handle_info({:EXIT, _pid, _reason}, state), do: {:noreply, state}

  defp start_bound_listener(path, socket, identity, owner, opts) do
    with :ok <- maybe_fail(opts, :chmod),
         :ok <- File.chmod(path, 0o600),
         :ok <- maybe_fail(opts, :supervisor),
         {:ok, sup} <- Task.Supervisor.start_link() do
      Process.unlink(sup)

      with :ok <- maybe_fail(opts, :acceptor),
           {:ok, acceptor} <-
             Task.Supervisor.start_child(sup, fn -> Acceptor.accept_loop(socket, sup) end) do
        {:ok,
         %{
           path: path,
           socket: socket,
           identity: identity,
           owner: owner,
           session_supervisor: sup,
           acceptor: acceptor,
           acceptor_ref: Process.monitor(acceptor)
         }}
      else
        {:error, r} ->
          Process.exit(sup, :shutdown)
          cleanup_failed(path, socket, identity, owner)
          {:error, r}
      end
    else
      {:error, r} ->
        cleanup_failed(path, socket, identity, owner)
        {:error, r}
    end
  end

  defp cleanup_failed(path, socket, identity, owner) do
    :gen_tcp.close(socket)
    cleanup_owner(path, owner, identity)
  end

  defp bind_listener(path, owner, opts) do
    with :ok <- prepare_socket_path(path, owner),
         :ok <- maybe_fail(opts, :listen),
         {:ok, socket} <- listen(path) do
      bind_socket(path, socket, owner, opts)
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp bind_socket(path, socket, owner, opts) do
    case socket_identity(path) do
      {:ok, identity} ->
        with :ok <- maybe_fail(opts, :identity),
             :ok <- maybe_fail(opts, :owner_write),
             {:ok, owner} <- write_owner(owner, identity),
             {:ok, state} <- start_bound_listener(path, socket, identity, owner, opts) do
          {:ok, state}
        else
          {:error, reason} ->
            cleanup_failed(path, socket, identity, owner)
            {:error, reason}
        end

      {:error, reason} ->
        :gen_tcp.close(socket)
        cleanup_owner(path, owner)
        {:error, reason}
    end
  end

  defp acquire_owner(path) do
    marker = path <> ".owner"

    with_claim(marker, fn ->
      case File.lstat(marker) do
        {:error, :enoent} -> install_provisional_owner(marker)
        {:ok, _} -> reclaim_or_reject(path, marker)
        {:error, reason} -> {:error, reason}
      end
    end)
  end

  defp reclaim_or_reject(path, marker) do
    with {:ok, record} <- read_marker(marker),
         false <- pid_alive?(record["pid"]),
         :ok <- reclaim_recorded_socket(path, record),
         {:ok, owner} <- install_provisional_owner(marker) do
      {:ok, owner}
    else
      true -> {:error, :socket_in_use}
      {:error, _} = error -> error
      _ -> {:error, :ambiguous_socket_ownership}
    end
  end

  defp reclaim_recorded_socket(path, %{"identity" => identity}) when is_list(identity) do
    expected = List.to_tuple(identity)

    case File.lstat(path) do
      {:error, :enoent} ->
        :ok

      {:ok, stat} ->
        if(socket?(stat) and identity(stat) == expected,
          do: File.rm(path),
          else: {:error, :ambiguous_socket_path}
        )

      _ ->
        {:error, :ambiguous_socket_path}
    end
  end

  defp reclaim_recorded_socket(_path, _), do: {:error, :ambiguous_socket_ownership}

  defp prepare_socket_path(path, _owner) do
    case File.lstat(path) do
      {:error, :enoent} -> :ok
      {:ok, _} -> {:error, :ambiguous_socket_path}
      {:error, r} -> {:error, r}
    end
  end

  defp install_provisional_owner(marker) do
    owner = %{
      marker: marker,
      claim: marker <> ".claim",
      nonce: Ecto.UUID.generate(),
      identity: nil
    }

    case atomic_write_marker(marker, marker_record(owner)) do
      :ok -> {:ok, owner}
      {:error, reason} -> {:error, reason}
    end
  end

  defp write_owner(owner, identity) do
    with_claim(owner.marker, fn ->
      with {:ok, record} <- read_marker(owner.marker),
           true <- matching_owner?(record, owner),
           :ok <- atomic_write_marker(owner.marker, marker_record(%{owner | identity: identity})) do
        {:ok, %{owner | identity: identity}}
      else
        false -> {:error, :socket_ownership_lost}
        {:error, _} = error -> error
      end
    end)
  end

  defp cleanup_owner(path, owner, socket_identity \\ nil) do
    _ =
      with_claim(owner.marker, fn ->
        with {:ok, record} <- read_marker(owner.marker), true <- matching_owner?(record, owner) do
          if socket_identity, do: safe_remove_socket(path, socket_identity)
          File.rm(owner.marker)
        else
          _ -> :ok
        end
      end)

    :ok
  end

  defp with_claim(marker, fun, attempts \\ 4)

  defp with_claim(marker, fun, attempts) do
    claim = marker <> ".claim"

    case File.mkdir(claim) do
      :ok ->
        try do
          case File.chmod(claim, @claim_mode) do
            :ok -> fun.()
            {:error, reason} -> {:error, reason}
          end
        after
          File.rmdir(claim)
        end

      {:error, :eexist} when attempts > 0 ->
        Process.sleep(5)
        with_claim(marker, fun, attempts - 1)

      {:error, :eexist} ->
        {:error, :socket_ownership_contended}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp atomic_write_marker(marker, record) do
    temp = marker <> ".tmp-" <> Ecto.UUID.generate()
    payload = Jason.encode!(record)

    result =
      with {:ok, io} <- File.open(temp, [:write, :exclusive, :binary]),
           :ok <- File.chmod(temp, @marker_mode),
           :ok <- IO.binwrite(io, payload),
           :ok <- File.close(io),
           :ok <- File.rename(temp, marker) do
        :ok
      end

    if result != :ok, do: File.rm(temp)
    result
  end

  defp read_marker(marker) do
    with {:ok, stat} <- File.lstat(marker),
         true <- trusted_marker_stat?(stat),
         {:ok, json} <- File.read(marker),
         {:ok, record} <- Jason.decode(json),
         true <- valid_marker_record?(record) do
      {:ok, record}
    else
      _ -> {:error, :ambiguous_socket_ownership}
    end
  end

  defp marker_record(owner),
    do: %{
      "version" => @marker_version,
      "pid" => os_pid(),
      "nonce" => owner.nonce,
      "identity" => if(owner.identity, do: Tuple.to_list(owner.identity), else: nil)
    }

  defp matching_owner?(record, owner),
    do: record["nonce"] == owner.nonce and record["identity"] == owner_identity(owner)

  defp owner_identity(%{identity: nil}), do: nil
  defp owner_identity(%{identity: identity}), do: Tuple.to_list(identity)

  defp trusted_marker_stat?(%{type: :regular, uid: uid, mode: mode, size: size}),
    do:
      uid == current_uid() and Bitwise.band(mode, 0o777) == @marker_mode and
        size <= @max_marker_bytes

  defp trusted_marker_stat?(_), do: false

  defp valid_marker_record?(record) when is_map(record) do
    MapSet.new(Map.keys(record)) == MapSet.new(["version", "pid", "nonce", "identity"]) and
      record["version"] == @marker_version and valid_pid?(record["pid"]) and
      valid_nonce?(record["nonce"]) and
      valid_identity?(record["identity"])
  end

  defp valid_marker_record?(_), do: false
  defp valid_pid?(pid) when is_binary(pid), do: String.match?(pid, ~r/^[1-9][0-9]{0,9}$/)
  defp valid_pid?(_), do: false
  defp valid_nonce?(nonce) when is_binary(nonce), do: match?({:ok, _}, Ecto.UUID.cast(nonce))
  defp valid_nonce?(_), do: false
  defp valid_identity?(nil), do: true

  defp valid_identity?([a, b, c, @socket_file_type, uid]) do
    Enum.all?([a, b, c, uid], &(is_integer(&1) and &1 >= 0 and &1 <= @max_identity_value)) and
      uid == current_uid()
  end

  defp valid_identity?(_), do: false

  defp safe_remove_socket(path, expected) do
    case File.lstat(path) do
      {:ok, stat} ->
        if(socket?(stat) and identity(stat) == expected, do: File.rm(path), else: :ok)

      _ ->
        :ok
    end
  end

  defp listen(path),
    do:
      :gen_tcp.listen(0, [
        :binary,
        packet: 4,
        active: false,
        reuseaddr: true,
        packet_size: @max_frame_bytes,
        ifaddr: {:local, String.to_charlist(path)}
      ])

  defp socket_identity(path) do
    case File.lstat(path) do
      {:ok, stat} ->
        if(socket?(stat), do: {:ok, identity(stat)}, else: {:error, :socket_identity_unavailable})

      _ ->
        {:error, :socket_identity_unavailable}
    end
  end

  defp socket?(%{mode: mode, uid: uid}),
    do: Bitwise.band(mode, @file_type_mask) == @socket_file_type and uid == current_uid()

  defp identity(%{major_device: a, minor_device: b, inode: c, mode: d, uid: e}),
    do: {a, b, c, Bitwise.band(d, @file_type_mask), e}

  defp maybe_fail(opts, stage),
    do:
      if(Keyword.get(opts, :fail_stage) == stage,
        do: {:error, {:injected_failure, stage}},
        else: :ok
      )

  defp os_pid, do: :os.getpid() |> List.to_string()

  defp pid_alive?(pid) when is_binary(pid) do
    case System.cmd("kill", ["-0", pid], stderr_to_stdout: true) do
      {_, 0} -> true
      _ -> false
    end
  end

  defp pid_alive?(_), do: true

  defp current_uid,
    do: System.cmd("id", ["-u"]) |> elem(0) |> String.trim() |> String.to_integer()
end

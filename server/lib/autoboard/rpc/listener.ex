defmodule Autoboard.RPC.Listener do
  @moduledoc false
  use GenServer
  alias Autoboard.RPC.Acceptor
  @max_frame_bytes 4_194_304
  @socket_file_type 0o140000
  @file_type_mask 0o170000

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, opts, Keyword.take(opts, [:name]))

  @impl true
  def init(opts) do
    path = Keyword.get(opts, :path, Application.fetch_env!(:autoboard, :socket_path))

    with :ok <- File.mkdir_p(Path.dirname(path)),
         {:ok, owner} <- acquire_owner(path),
         :ok <- prepare_socket_path(path, owner),
         {:ok, socket} <- listen(path),
         {:ok, identity} <- socket_identity(path),
         :ok <- write_owner(owner, identity),
         {:ok, state} <- start_bound_listener(path, socket, identity, owner, opts) do
      {:ok, state}
    else
      {:error, reason} -> {:stop, reason}
    end
  end

  @impl true
  def terminate(_reason, state) do
    :gen_tcp.close(state.socket)
    Process.exit(state.session_supervisor, :shutdown)

    if owns?(state.owner, state.identity),
      do:
        (
          safe_remove_socket(state.path, state.identity)
          File.rm(state.owner.marker)
        )

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

    if owns?(owner, identity),
      do:
        (
          safe_remove_socket(path, identity)
          File.rm(owner.marker)
        )
  end

  defp acquire_owner(path), do: acquire_owner(path, 2)
  defp acquire_owner(_path, 0), do: {:error, :socket_ownership_contended}

  defp acquire_owner(path, attempts) do
    marker = path <> ".owner"
    nonce = Ecto.UUID.generate()
    record = %{"pid" => os_pid(), "nonce" => nonce, "identity" => nil}

    case File.open(marker, [:write, :exclusive]) do
      {:ok, io} ->
        :ok = IO.binwrite(io, Jason.encode!(record))
        :ok = File.close(io)
        :ok = File.chmod(marker, 0o600)
        {:ok, %{marker: marker, nonce: nonce}}

      {:error, :eexist} ->
        reclaim_or_reject(path, marker, attempts)

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp reclaim_or_reject(path, marker, attempts) do
    with {:ok, json} <- File.read(marker),
         {:ok, record} <- Jason.decode(json),
         false <- pid_alive?(record["pid"]) do
      tomb = marker <> ".stale-" <> Ecto.UUID.generate()

      case File.rename(marker, tomb) do
        :ok ->
          result = reclaim_recorded_socket(path, record)
          File.rm(tomb)

          case result do
            :ok -> acquire_owner(path, attempts - 1)
            {:error, _} = error -> error
          end

        {:error, _} ->
          acquire_owner(path, attempts - 1)
      end
    else
      true -> {:error, :socket_in_use}
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

  defp write_owner(owner, identity),
    do:
      File.write(
        owner.marker,
        Jason.encode!(%{
          "pid" => os_pid(),
          "nonce" => owner.nonce,
          "identity" => Tuple.to_list(identity)
        })
      )

  defp owns?(owner, identity) do
    with {:ok, json} <- File.read(owner.marker), {:ok, record} <- Jason.decode(json) do
      record["nonce"] == owner.nonce and record["identity"] == Tuple.to_list(identity)
    else
      _ -> false
    end
  end

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

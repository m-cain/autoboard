defmodule Autoboard.RPC.Listener do
  @moduledoc false

  use GenServer

  alias Autoboard.RPC.Acceptor

  @max_frame_bytes 4_194_304
  @socket_file_type 0o140000
  @file_type_mask 0o170000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    path = Keyword.get(opts, :path, Application.fetch_env!(:autoboard, :socket_path))

    with :ok <- prepare_socket_path(path),
         {:ok, socket} <- listen(path),
         {:ok, identity} <- socket_identity(path) do
      case start_bound_listener(path, socket, identity, opts) do
        {:ok, state} ->
          {:ok, state}

        {:error, reason} ->
          :gen_tcp.close(socket)
          safe_remove_socket(path, identity)
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
    safe_remove_socket(state.path, state.identity)
    :ok
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{acceptor_ref: ref} = state) do
    case Task.Supervisor.start_child(state.session_supervisor, fn ->
           Acceptor.accept_loop(state.socket, state.session_supervisor)
         end) do
      {:ok, acceptor} ->
        {:noreply, %{state | acceptor: acceptor, acceptor_ref: Process.monitor(acceptor)}}

      {:error, _reason} ->
        {:stop, :acceptor_unavailable, state}
    end
  end

  defp listen(path) do
    :gen_tcp.listen(
      0,
      [
        :binary,
        packet: 4,
        active: false,
        reuseaddr: true,
        packet_size: @max_frame_bytes,
        ifaddr: {:local, String.to_charlist(path)}
      ]
    )
  end

  defp start_bound_listener(path, socket, identity, opts) do
    with :ok <- maybe_fail(opts, :chmod),
         :ok <- File.chmod(path, 0o600),
         :ok <- maybe_fail(opts, :supervisor),
         {:ok, session_supervisor} <- Task.Supervisor.start_link() do
      Process.unlink(session_supervisor)
      start_acceptor(path, socket, identity, session_supervisor, opts)
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp start_acceptor(path, socket, identity, session_supervisor, opts) do
    with :ok <- maybe_fail(opts, :acceptor),
         {:ok, acceptor} <-
           Task.Supervisor.start_child(session_supervisor, fn ->
             Acceptor.accept_loop(socket, session_supervisor)
           end) do
      {:ok,
       %{
         path: path,
         socket: socket,
         identity: identity,
         session_supervisor: session_supervisor,
         acceptor: acceptor,
         acceptor_ref: Process.monitor(acceptor)
       }}
    else
      {:error, reason} ->
        Process.exit(session_supervisor, :shutdown)
        {:error, reason}
    end
  end

  defp maybe_fail(opts, stage) do
    if Keyword.get(opts, :fail_stage) == stage,
      do: {:error, {:injected_failure, stage}},
      else: :ok
  end

  defp prepare_socket_path(path) do
    with :ok <- File.mkdir_p(Path.dirname(path)) do
      case File.lstat(path) do
        {:error, :enoent} -> :ok
        {:ok, stat} -> remove_stale_socket(path, stat)
        {:error, reason} -> {:error, reason}
      end
    end
  end

  defp remove_stale_socket(path, stat) do
    if socket_owned_by_current_user?(stat) do
      identity = identity(stat)

      case probe_socket(path) do
        :live -> {:error, :socket_in_use}
        :stale -> safe_remove_socket(path, identity)
      end
    else
      {:error, :unsafe_existing_socket_path}
    end
  end

  defp safe_remove_socket(path, expected_identity) do
    case File.lstat(path) do
      {:ok, stat} when is_map(stat) ->
        if socket_owned_by_current_user?(stat) and identity(stat) == expected_identity,
          do: File.rm(path),
          else: :ok

      _ ->
        :ok
    end
  end

  defp probe_socket(path) do
    case :gen_tcp.connect({:local, String.to_charlist(path)}, 0, [:binary, active: false], 100) do
      {:ok, socket} ->
        :gen_tcp.close(socket)
        :live

      {:error, _reason} ->
        :stale
    end
  end

  defp socket_identity(path) do
    case File.lstat(path) do
      {:ok, stat} when is_map(stat) ->
        if socket_owned_by_current_user?(stat),
          do: {:ok, identity(stat)},
          else: {:error, :socket_identity_unavailable}

      _ ->
        {:error, :socket_identity_unavailable}
    end
  end

  defp identity(%{major_device: major, minor_device: minor, inode: inode, mode: mode, uid: uid}),
    do: {major, minor, inode, Bitwise.band(mode, @file_type_mask), uid}

  defp socket_owned_by_current_user?(%{mode: mode, uid: uid}) do
    Bitwise.band(mode, @file_type_mask) == @socket_file_type and uid == current_uid()
  end

  defp socket_owned_by_current_user?(_stat), do: false

  defp current_uid do
    {uid, 0} = System.cmd("id", ["-u"])
    uid |> String.trim() |> String.to_integer()
  end
end

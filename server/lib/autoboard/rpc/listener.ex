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
         :ok <- File.chmod(path, 0o600),
         {:ok, session_supervisor} <- Task.Supervisor.start_link() do
      {:ok, acceptor} =
        Task.Supervisor.start_child(session_supervisor, fn ->
          Acceptor.accept_loop(socket, session_supervisor)
        end)

      {:ok,
       %{path: path, socket: socket, session_supervisor: session_supervisor, acceptor: acceptor}}
    else
      {:error, reason} -> {:stop, reason}
    end
  end

  @impl true
  def terminate(_reason, state) do
    :gen_tcp.close(state.socket)
    safe_remove_socket(state.path)
    :ok
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
      File.rm(path)
    else
      {:error, :unsafe_existing_socket_path}
    end
  end

  defp safe_remove_socket(path) do
    case File.lstat(path) do
      {:ok, stat} when is_map(stat) ->
        if socket_owned_by_current_user?(stat), do: File.rm(path), else: :ok

      _ ->
        :ok
    end
  end

  defp socket_owned_by_current_user?(%{mode: mode, uid: uid}) do
    Bitwise.band(mode, @file_type_mask) == @socket_file_type and uid == current_uid()
  end

  defp socket_owned_by_current_user?(_stat), do: false

  defp current_uid do
    {uid, 0} = System.cmd("id", ["-u"])
    uid |> String.trim() |> String.to_integer()
  end
end

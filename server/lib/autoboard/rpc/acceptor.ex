defmodule Autoboard.RPC.Acceptor do
  @moduledoc false

  alias Autoboard.RPC.Session

  def accept_loop(listen_socket, session_supervisor) do
    case :gen_tcp.accept(listen_socket) do
      {:ok, socket} ->
        case Task.Supervisor.start_child(session_supervisor, &Session.await_socket/0) do
          {:ok, session} ->
            :ok = :gen_tcp.controlling_process(socket, session)
            send(session, {:socket, socket})

          {:error, _reason} ->
            :gen_tcp.close(socket)
        end

        accept_loop(listen_socket, session_supervisor)

      {:error, :closed} ->
        :ok

      {:error, _reason} ->
        :ok
    end
  end
end

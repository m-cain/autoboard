defmodule Autoboard.RPC.Session do
  @moduledoc """
  A single private RPC connection.

  JSON-RPC notifications (requests without an `id`) are processed after a
  successful initialization but intentionally produce no response, including
  when their method or parameters are invalid. Invalid IDs receive an invalid
  request response with a null ID. Initialization must be a request so callers
  can observe authentication and version errors.
  """

  require Logger

  alias Autoboard.Auth.Token
  alias Autoboard.Domain.Error, as: DomainError
  alias Autoboard.RPC.Error
  alias Autoboard.RPC.Router

  @protocol_version 1
  @max_frame_bytes 4_194_304

  def await_socket do
    receive do
      {:socket, socket} -> loop(socket, nil)
    after
      1_000 -> :ok
    end
  end

  defp loop(socket, context) do
    case :gen_tcp.recv(socket, 0) do
      {:ok, payload} when byte_size(payload) <= @max_frame_bytes ->
        case handle_payload(payload, context) do
          {:continue, next_context, response} ->
            send_response(socket, response)
            loop(socket, next_context)

          {:close, response} ->
            send_response(socket, response)
            :gen_tcp.close(socket)
        end

      {:ok, _payload} ->
        :gen_tcp.close(socket)

      {:error, :emsgsize} ->
        :gen_tcp.close(socket)

      {:error, _reason} ->
        :ok
    end
  rescue
    error ->
      correlation_id = Ecto.UUID.generate()

      Logger.error(
        "rpc session failure correlation_id=#{correlation_id}: #{Exception.message(error)}"
      )

      send_response(socket, Error.internal(nil, correlation_id))
      :gen_tcp.close(socket)
  catch
    kind, reason ->
      correlation_id = Ecto.UUID.generate()
      Logger.error("rpc session #{kind} correlation_id=#{correlation_id}: #{inspect(reason)}")
      send_response(socket, Error.internal(nil, correlation_id))
      :gen_tcp.close(socket)
  end

  defp handle_payload(payload, context) do
    case Jason.decode(payload) do
      {:ok, request} -> handle_request(request, context)
      {:error, _reason} -> {:continue, context, Error.invalid_request(nil, "Malformed JSON")}
    end
  end

  defp handle_request(request, context) when is_map(request) do
    with {:ok, id, notification?} <- request_id(request),
         :ok <- valid_envelope(request, id),
         {:ok, params} <- params(request, id) do
      dispatch(request["method"], params, id, notification?, context)
    else
      {:error, id, message} -> {:continue, context, Error.invalid_request(id, message)}
    end
  end

  defp handle_request(_request, context), do: {:continue, context, Error.invalid_request(nil)}

  defp dispatch("session.initialize", params, id, notification?, nil) do
    case initialize(params) do
      {:ok, context, result} ->
        {:continue, context, maybe_response(notification?, result_envelope(id, result))}

      {:error, :close, response} ->
        {:close, maybe_response(notification?, Map.put(response, "id", id))}
    end
  end

  defp dispatch("session.initialize", _params, id, notification?, context) do
    {:continue, context,
     maybe_response(notification?, Error.invalid_request(id, "Session is already initialized"))}
  end

  defp dispatch(_method, _params, id, notification?, nil) do
    {:close,
     maybe_response(
       notification?,
       Error.invalid_request(id, "session.initialize must be the first request")
     )}
  end

  defp dispatch(method, params, id, notification?, context) do
    response =
      case Router.dispatch(context, method, params) do
        {:ok, result} -> result_envelope(id, result)
        {:error, %DomainError{kind: :method_not_found}} -> Error.method_not_found(id)
        {:error, %DomainError{} = error} -> Error.domain(id, error)
      end

    {:continue, context, maybe_response(notification?, response)}
  rescue
    error ->
      correlation_id = Ecto.UUID.generate()

      Logger.error(
        "rpc router failure correlation_id=#{correlation_id}: #{Exception.message(error)}"
      )

      {:continue, context, maybe_response(notification?, Error.internal(id, correlation_id))}
  end

  defp initialize(%{
         "protocol_version" => @protocol_version,
         "token" => token,
         "client" => client
       })
       when is_binary(token) and is_map(client) do
    case Token.authenticate(token) do
      {:ok, context} ->
        {:ok, context,
         %{
           "protocol_version" => @protocol_version,
           "server_version" => version(),
           "actor" => Atom.to_string(context.actor),
           "authorization" => %{"kind" => Atom.to_string(context.scope)}
         }}

      {:error, %DomainError{} = error} ->
        {:error, :close, Error.domain(nil, error)}
    end
  end

  defp initialize(%{"protocol_version" => version}) when is_integer(version) do
    error = %DomainError{
      kind: :validation_failed,
      message: "unsupported protocol version",
      fields: %{protocol_version: ["must equal #{@protocol_version}"]}
    }

    {:error, :close, Error.invalid_params(nil, error)}
  end

  defp initialize(_params) do
    error = %DomainError{
      kind: :validation_failed,
      message: "invalid initialization parameters",
      fields: %{base: ["protocol_version, token, and client are required"]}
    }

    {:error, :close, Error.invalid_params(nil, error)}
  end

  defp valid_envelope(%{"jsonrpc" => "2.0", "method" => method}, _id) when is_binary(method),
    do: :ok

  defp valid_envelope(_request, id), do: {:error, id, "Invalid JSON-RPC envelope"}

  defp params(%{"params" => params}, _id) when is_map(params), do: {:ok, params}
  defp params(%{"params" => _params}, id), do: {:error, id, "params must be an object"}
  defp params(_request, id), do: {:error, id, "params is required"}

  defp request_id(request) do
    case Map.fetch(request, "id") do
      :error -> {:ok, nil, true}
      {:ok, id} when is_binary(id) or is_integer(id) -> {:ok, id, false}
      {:ok, nil} -> {:ok, nil, true}
      {:ok, _id} -> {:error, nil, "id must be a string, number, or null"}
    end
  end

  defp maybe_response(true, _response), do: nil
  defp maybe_response(false, response), do: response
  defp send_response(_socket, nil), do: :ok
  defp send_response(socket, response), do: :gen_tcp.send(socket, Jason.encode!(response))
  defp result_envelope(id, result), do: %{"jsonrpc" => "2.0", "id" => id, "result" => result}
  defp version, do: Application.spec(:autoboard, :vsn) |> to_string()
end

defmodule Autoboard.RPCClient do
  @moduledoc false

  @max_frame_bytes 4_194_304

  def connect(path) do
    :gen_tcp.connect({:local, String.to_charlist(path)}, 0, [
      :binary,
      packet: 0,
      active: false,
      packet_size: @max_frame_bytes
    ])
  end

  def send(socket, request), do: send_raw_frame(socket, Jason.encode!(request))

  def receive(socket, timeout \\ 1_000) do
    with {:ok, <<length::unsigned-big-integer-size(32)>>} <- :gen_tcp.recv(socket, 4, timeout),
         {:ok, json} <- :gen_tcp.recv(socket, length, timeout) do
      decode({:ok, json})
    end
  end

  def send_raw_frame(socket, json) when is_binary(json) do
    :gen_tcp.send(socket, <<byte_size(json)::unsigned-big-integer-size(32), json::binary>>)
  end

  def send_coalesced(socket, requests) do
    payload =
      Enum.map_join(requests, fn request ->
        json = Jason.encode!(request)
        <<byte_size(json)::unsigned-big-integer-size(32), json::binary>>
      end)

    :gen_tcp.send(socket, payload)
  end

  def send_fragmented(socket, request, split_at) do
    json = Jason.encode!(request)
    frame = <<byte_size(json)::unsigned-big-integer-size(32), json::binary>>
    <<first::binary-size(split_at), rest::binary>> = frame
    :ok = :gen_tcp.send(socket, first)
    :gen_tcp.send(socket, rest)
  end

  defp decode({:ok, json}), do: Jason.decode(json)
  defp decode(other), do: other
end

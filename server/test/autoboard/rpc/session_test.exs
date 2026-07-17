defmodule Autoboard.RPC.SessionTest do
  use Autoboard.DataCase, async: false

  alias Autoboard.Auth.Token
  alias Autoboard.RPCClient

  @max_frame_bytes 4_194_304

  setup do
    path =
      Path.join(System.tmp_dir!(), "autoboard-rpc-#{System.unique_integer([:positive])}.sock")

    on_exit(fn -> remove_owned_socket(path) end)

    {:ok, listener} = start_supervised({Autoboard.RPC.Listener, path: path})
    assert eventually(fn -> File.exists?(path) end)
    {:ok, token, _record} = Token.issue(:codex)

    %{listener: listener, path: path, token: token}
  end

  test "requires initialization before routing requests", %{path: path} do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, request(1, "projects.list", %{}))

    assert {:ok,
            %{"id" => 1, "error" => %{"code" => -32600, "data" => %{"kind" => "invalid_request"}}}} =
             RPCClient.receive(socket)

    assert {:error, :closed} = :gen_tcp.recv(socket, 0, 1_000)
  end

  test "initializes with a valid token and keeps the authenticated actor server-side", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, initialize(1, token))

    assert {:ok,
            %{
              "jsonrpc" => "2.0",
              "id" => 1,
              "result" => %{
                "protocol_version" => 1,
                "server_version" => "0.1.0",
                "actor" => "codex",
                "authorization" => %{"kind" => "global"}
              }
            }} = RPCClient.receive(socket)

    :ok =
      RPCClient.send(
        socket,
        request(2, "projects.list", %{"actor" => "me", "scope" => "project"})
      )

    assert {:ok, %{"id" => 2, "result" => %{"active" => [], "archived" => []}}} =
             RPCClient.receive(socket)
  end

  test "returns unauthorized and closes after an invalid token", %{path: path} do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, initialize(1, "wrong-token"))

    assert {:ok,
            %{"id" => 1, "error" => %{"code" => -32010, "data" => %{"kind" => "unauthorized"}}}} =
             RPCClient.receive(socket)

    assert {:error, :closed} = :gen_tcp.recv(socket, 0, 1_000)
  end

  test "returns validation error and closes after a protocol-version mismatch", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, initialize(1, token, 2))

    assert {:ok,
            %{
              "id" => 1,
              "error" => %{"code" => -32602, "data" => %{"kind" => "validation_failed"}}
            }} =
             RPCClient.receive(socket)

    assert {:error, :closed} = :gen_tcp.recv(socket, 0, 1_000)
  end

  test "handles a frame fragmented between its length prefix and payload", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send_fragmented(socket, initialize(1, token), 2)

    assert {:ok, %{"id" => 1, "result" => %{"actor" => "codex"}}} = RPCClient.receive(socket)
  end

  test "handles coalesced packet:4 request frames and preserves response IDs", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)

    :ok =
      RPCClient.send_coalesced(socket, [initialize(10, token), request(11, "projects.list", %{})])

    assert {:ok, %{"id" => 10, "result" => %{"protocol_version" => 1}}} =
             RPCClient.receive(socket)

    assert {:ok, %{"id" => 11, "result" => %{"active" => [], "archived" => []}}} =
             RPCClient.receive(socket)
  end

  test "supports distinct concurrent request IDs on one initialized connection", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, initialize(1, token))
    assert {:ok, _} = RPCClient.receive(socket)

    :ok =
      RPCClient.send_coalesced(socket, [
        request("first", "projects.list", %{}),
        request(99, "projects.list", %{})
      ])

    assert {:ok, first} = RPCClient.receive(socket)
    assert {:ok, second} = RPCClient.receive(socket)
    assert MapSet.new([first["id"], second["id"]]) == MapSet.new(["first", 99])
  end

  test "returns JSON-RPC errors for malformed JSON, malformed envelopes, and unknown methods", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send_raw_frame(socket, "{not json")
    assert {:ok, %{"id" => nil, "error" => %{"code" => -32600}}} = RPCClient.receive(socket)

    :ok = RPCClient.send(socket, %{"jsonrpc" => "2.0", "id" => 2, "method" => "projects.list"})
    assert {:ok, %{"id" => 2, "error" => %{"code" => -32600}}} = RPCClient.receive(socket)

    :ok = RPCClient.send(socket, initialize(3, token))
    assert {:ok, %{"id" => 3, "result" => _}} = RPCClient.receive(socket)

    :ok = RPCClient.send(socket, request(4, "no.such.method", %{}))
    assert {:ok, %{"id" => 4, "error" => %{"code" => -32601}}} = RPCClient.receive(socket)
  end

  test "processes notifications without replies and rejects non-scalar request IDs", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, initialize(1, token))
    assert {:ok, _} = RPCClient.receive(socket)

    :ok =
      RPCClient.send(socket, %{"jsonrpc" => "2.0", "method" => "projects.list", "params" => %{}})

    assert {:error, :timeout} = RPCClient.receive(socket, 50)

    :ok =
      RPCClient.send(socket, %{
        "jsonrpc" => "2.0",
        "id" => %{},
        "method" => "projects.list",
        "params" => %{}
      })

    assert {:ok, %{"id" => nil, "error" => %{"code" => -32600}}} = RPCClient.receive(socket)
  end

  test "treats only an absent id as a notification and never initializes from one", %{
    path: path,
    token: token
  } do
    {:ok, socket} = RPCClient.connect(path)

    :ok =
      RPCClient.send(socket, %{
        "jsonrpc" => "2.0",
        "method" => "session.initialize",
        "params" => initialize(1, token)["params"]
      })

    assert {:error, :closed} = RPCClient.receive(socket)

    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, Map.put(initialize(1, token), "id", nil))
    assert {:ok, %{"id" => nil, "error" => %{"code" => -32600}}} = RPCClient.receive(socket)
  end

  test "uses invalid params for object methods with non-object params and suppresses notification failures",
       %{
         path: path,
         token: token
       } do
    {:ok, socket} = RPCClient.connect(path)
    :ok = RPCClient.send(socket, initialize(1, token))
    assert {:ok, _} = RPCClient.receive(socket)

    :ok =
      RPCClient.send(socket, %{
        "jsonrpc" => "2.0",
        "id" => 2,
        "method" => "projects.list",
        "params" => []
      })

    assert {:ok, %{"id" => 2, "error" => %{"code" => -32602}}} = RPCClient.receive(socket)

    :ok =
      RPCClient.send(socket, %{"jsonrpc" => "2.0", "method" => "missing.method", "params" => %{}})

    assert {:error, :timeout} = RPCClient.receive(socket, 50)
  end

  test "rejects oversized frames without leaving the session open", %{path: path} do
    {:ok, socket} = RPCClient.connect(path)
    :ok = :gen_tcp.send(socket, <<@max_frame_bytes + 1::unsigned-big-integer-size(32)>>)
    assert {:error, :closed} = :gen_tcp.recv(socket, 0, 1_000)
  end

  test "creates an owner-only socket and removes it when stopped", %{
    listener: listener,
    path: path
  } do
    assert {:ok, %{type: :other, mode: mode}} = File.lstat(path)
    assert Bitwise.band(mode, 0o777) == 0o600
    :ok = GenServer.stop(listener)
    refute File.exists?(path)
  end

  defp initialize(id, token, version \\ 1) do
    request(id, "session.initialize", %{
      "protocol_version" => version,
      "token" => token,
      "client" => %{"name" => "autoboard-test", "version" => "1.0.0"}
    })
  end

  defp request(id, method, params),
    do: %{"jsonrpc" => "2.0", "id" => id, "method" => method, "params" => params}

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

  defp remove_owned_socket(path) do
    case File.lstat(path) do
      {:ok, %{type: :other, mode: mode}} when Bitwise.band(mode, 0o170000) == 0o140000 ->
        File.rm(path)

      _ ->
        :ok
    end
  end
end

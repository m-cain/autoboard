defmodule Autoboard.ApplicationTest do
  use ExUnit.Case, async: false

  test "starts the repository under the application supervisor" do
    assert Process.whereis(Autoboard.Repo)
    assert Process.whereis(Autoboard.Activity.Registry)
    assert Process.whereis(Autoboard.Attachments.Cleanup)
    assert %{active: active} = Supervisor.count_children(Autoboard.Supervisor)
    assert active >= 3
  end

  test "the test suite owns an isolated temporary data directory" do
    data_dir = Application.fetch_env!(:autoboard, :data_dir)
    socket_path = Application.fetch_env!(:autoboard, :socket_path)

    assert String.starts_with?(data_dir, Path.join(System.tmp_dir!(), "autoboard-test-"))
    assert socket_path == Path.join(data_dir, "autoboard.sock")
    refute String.contains?(data_dir, "/server/var")
  end

  test "compose exposes its configurable PostgreSQL port only on loopback" do
    compose = File.read!(Path.expand("../../../compose.yaml", __DIR__))

    assert compose =~ ~s(POSTGRES_DB: ${AUTOBOARD_DB_NAME:-autoboard})
    assert compose =~ ~s(POSTGRES_USER: ${AUTOBOARD_DB_USER:-autoboard})
    assert compose =~ ~s(POSTGRES_PASSWORD: ${AUTOBOARD_DB_PASSWORD:-autoboard})
    assert compose =~ ~s("127.0.0.1:${AUTOBOARD_DB_PORT:-5432}:5432")
    refute compose =~ ~s(- "5432:5432")
  end
end

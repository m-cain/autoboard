defmodule Mix.Tasks.AutoboardBootstrapTest do
  use Autoboard.DataCase, async: false

  import Bitwise
  import ExUnit.CaptureIO

  alias Autoboard.Auth.Token
  alias Autoboard.Repo

  setup do
    previous_data_dir = Application.fetch_env!(:autoboard, :data_dir)

    data_dir =
      Path.join(System.tmp_dir!(), "autoboard-bootstrap-#{System.unique_integer([:positive])}")

    Application.put_env(:autoboard, :data_dir, data_dir)

    on_exit(fn ->
      Application.put_env(:autoboard, :data_dir, previous_data_dir)
      File.rm_rf(data_dir)
    end)

    %{data_dir: data_dir}
  end

  test "setup migrates and creates owner-only managed attachment directories idempotently", %{
    data_dir: data_dir
  } do
    assert :ok = Mix.Tasks.Autoboard.Setup.run([])
    assert :ok = Mix.Tasks.Autoboard.Setup.run([])

    for path <- [
          data_dir,
          Path.join(data_dir, "attachments"),
          Path.join(data_dir, "attachments/tmp")
        ] do
      assert {:ok, stat} = File.stat(path)
      assert (stat.mode &&& 0o777) == 0o700
    end
  end

  test "token task prints exactly one plaintext token and persists only its digest" do
    stdout =
      capture_io(fn ->
        assert :ok = Mix.Tasks.Autoboard.Token.Create.run(["--actor", "codex"])
      end)

    assert [token] = String.split(stdout, "\n", trim: true)
    assert token =~ ~r/^ab_[A-Za-z0-9_-]+$/
    assert byte_size(token) == 46

    assert %Token{actor: :codex, digest: digest} =
             Repo.get_by!(Token, actor: :codex, digest: :crypto.hash(:sha256, token))

    refute digest == token
  end

  test "token task accepts only an explicit me or codex actor" do
    assert_raise Mix.Error, ~r/--actor must be me or codex/, fn ->
      Mix.Tasks.Autoboard.Token.Create.run(["--actor", "system"])
    end

    assert_raise Mix.Error, ~r/requires --actor/, fn ->
      Mix.Tasks.Autoboard.Token.Create.run([])
    end

    assert_raise Mix.Error, ~r/accepts only --actor/, fn ->
      Mix.Tasks.Autoboard.Token.Create.run(["--actor", "me", "--actor", "codex"])
    end
  end
end

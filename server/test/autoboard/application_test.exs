defmodule Autoboard.ApplicationTest do
  use ExUnit.Case, async: false

  test "starts the repository under the application supervisor" do
    assert Process.whereis(Autoboard.Repo)
    assert Process.whereis(Autoboard.Activity.Registry)
    assert Process.whereis(Autoboard.Attachments.Cleanup)
    assert %{active: active} = Supervisor.count_children(Autoboard.Supervisor)
    assert active >= 3
  end
end

defmodule Autoboard.ApplicationTest do
  use ExUnit.Case, async: false

  test "starts the repository under the application supervisor" do
    assert Process.whereis(Autoboard.Repo)
    assert %{active: active} = Supervisor.count_children(Autoboard.Supervisor)
    assert active >= 1
  end
end

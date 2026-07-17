defmodule AutoboardTest do
  use ExUnit.Case
  doctest Autoboard

  test "greets the world" do
    assert Autoboard.hello() == :world
  end
end

defmodule Autoboard.DataCase do
  use ExUnit.CaseTemplate

  using do
    quote do
      alias Autoboard.Repo
    end
  end

  setup tags do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(Autoboard.Repo)

    unless tags[:async] do
      Ecto.Adapters.SQL.Sandbox.mode(Autoboard.Repo, {:shared, self()})
    end

    :ok
  end
end

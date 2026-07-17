defmodule AutoboardWeb.SPATest do
  use ExUnit.Case, async: false

  import Plug.Test

  alias AutoboardWeb.Router
  alias AutoboardWeb.SPA

  @opts Router.init([])

  test "uses the application's relocated priv directory by default and supports an injected static bundle" do
    previous = Application.get_env(:autoboard, :static_dir)
    Application.delete_env(:autoboard, :static_dir)

    on_exit(fn ->
      if previous,
        do: Application.put_env(:autoboard, :static_dir, previous),
        else: Application.delete_env(:autoboard, :static_dir)
    end)

    assert SPA.static_dir() == Path.join(to_string(:code.priv_dir(:autoboard)), "static")

    bundle =
      Path.join(System.tmp_dir!(), "autoboard-static-#{System.unique_integer([:positive])}")

    File.mkdir_p!(bundle)
    File.write!(Path.join(bundle, "index.html"), "<main>relocated release</main>")
    Application.put_env(:autoboard, :static_dir, bundle)

    on_exit(fn -> File.rm_rf(bundle) end)

    response = conn(:get, "/projects/RELEASE/tickets/1") |> Router.call(@opts)
    assert response.status == 200
    assert response.resp_body == "<main>relocated release</main>"
  end
end

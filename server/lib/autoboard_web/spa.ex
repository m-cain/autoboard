defmodule AutoboardWeb.SPA do
  @moduledoc false

  import Plug.Conn

  @spec send_index(Plug.Conn.t()) :: Plug.Conn.t()
  def send_index(conn) do
    path = Path.join(static_dir(), "index.html")

    if File.regular?(path) do
      conn
      |> put_resp_content_type("text/html")
      |> send_file(200, path)
    else
      send_resp(conn, 404, "not found")
    end
  end

  @spec static_dir() :: String.t()
  def static_dir do
    Application.get_env(:autoboard, :static_dir, default_static_dir())
  end

  defp default_static_dir do
    :autoboard
    |> :code.priv_dir()
    |> to_string()
    |> Path.join("static")
  end
end

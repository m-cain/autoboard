defmodule AutoboardWeb.JSON do
  @moduledoc false

  import Plug.Conn

  alias Autoboard.Domain.Error
  alias Autoboard.Presenter
  alias Autoboard.Repo

  @spec send(Plug.Conn.t(), non_neg_integer(), term()) :: Plug.Conn.t()
  def send(conn, status, payload) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(payload))
  end

  @spec error(Plug.Conn.t(), Error.t()) :: Plug.Conn.t()
  def error(conn, %Error{} = error),
    do: send(conn, status(error.kind), %{"error" => Presenter.error(error)})

  @spec health() :: :ok | {:error, term()}
  def health do
    case Application.get_env(:autoboard, :health_check) do
      fun when is_function(fun, 0) -> fun.()
      _ -> Repo.query("SELECT 1") |> health_result()
    end
  rescue
    _ -> {:error, :unavailable}
  catch
    _, _ -> {:error, :unavailable}
  end

  defp health_result({:ok, _result}), do: :ok
  defp health_result({:error, reason}), do: {:error, reason}
  defp health_result(other), do: other

  defp status(:not_found), do: 404
  defp status(:validation_failed), do: 400
  defp status(:unauthorized), do: 403
  defp status(_), do: 500
end

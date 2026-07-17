defmodule Autoboard.RPC.Error do
  @moduledoc false

  alias Autoboard.Domain.Error, as: DomainError
  alias Autoboard.Presenter

  def invalid_request(id, message \\ "Invalid Request") do
    envelope(id, -32600, message, %{"kind" => "invalid_request"})
  end

  def invalid_params(id, %DomainError{} = error) do
    envelope(id, -32602, "Invalid params", domain_data(error))
  end

  def method_not_found(id),
    do: envelope(id, -32601, "Method not found", %{"kind" => "method_not_found"})

  def domain(id, %DomainError{kind: :validation_failed} = error), do: invalid_params(id, error)

  def domain(id, %DomainError{} = error),
    do: envelope(id, -32010, error.message, domain_data(error))

  def internal(id, correlation_id) do
    envelope(id, -32010, "Internal error", %{
      "kind" => "internal_error",
      "correlation_id" => correlation_id
    })
  end

  defp envelope(id, code, message, data) do
    %{
      "jsonrpc" => "2.0",
      "id" => id,
      "error" => %{"code" => code, "message" => message, "data" => data}
    }
  end

  defp domain_data(error) do
    error
    |> Presenter.error()
    |> Map.reject(fn {key, value} -> key == "current" and is_nil(value) end)
  end
end

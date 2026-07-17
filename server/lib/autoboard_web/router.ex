defmodule AutoboardWeb.Router do
  @moduledoc false

  use Plug.Router

  alias Autoboard.Attachments
  alias Autoboard.Auth.Context
  alias Autoboard.Domain.Error
  alias Autoboard.Presenter
  alias Autoboard.ReadModel
  alias AutoboardWeb.EventsStream
  alias AutoboardWeb.JSON
  alias AutoboardWeb.SPA

  @project_key ~r/\A[A-Za-z][A-Za-z0-9]{1,7}\z/
  @ticket_identifier ~r/\A[A-Za-z][A-Za-z0-9]{1,7}-[1-9][0-9]*\z/

  plug(Plug.Static, at: "/", from: {:autoboard, "priv/static"}, only: [])
  plug(:match)
  plug(Plug.Parsers, parsers: [:json], pass: ["application/json"], json_decoder: Jason)
  plug(:dispatch)

  get "/api/v1/projects" do
    with {:ok, projects} <- ReadModel.list_projects(context()) do
      JSON.send(conn, 200, %{
        "active" => Enum.map(projects.active, &Presenter.project/1),
        "archived" => Enum.map(projects.archived, &Presenter.project/1)
      })
    else
      {:error, error} -> JSON.error(conn, error)
    end
  end

  get "/api/v1/triage" do
    ticket_list(conn, ReadModel.triage_tickets(context()))
  end

  get "/api/v1/projects/:key/board" do
    with :ok <- valid_project_key(key),
         {:ok, board} <- ReadModel.project_board(context(), key) do
      JSON.send(conn, 200, Presenter.board(board.project, board.columns))
    else
      {:error, error} -> JSON.error(conn, error)
    end
  end

  get "/api/v1/projects/:key/canceled" do
    with :ok <- valid_project_key(key),
         {:ok, tickets} <- ReadModel.canceled_tickets(context(), key) do
      JSON.send(conn, 200, %{"tickets" => Enum.map(tickets, &Presenter.ticket_summary/1)})
    else
      {:error, error} -> JSON.error(conn, error)
    end
  end

  get "/api/v1/tickets/:identifier" do
    with :ok <- valid_ticket_identifier(identifier),
         {:ok, detail} <- ReadModel.ticket_detail(context(), identifier) do
      JSON.send(conn, 200, Presenter.ticket_detail(detail))
    else
      {:error, error} -> JSON.error(conn, error)
    end
  end

  get "/api/v1/attachments/:id" do
    with {:ok, id} <- valid_uuid(id),
         {:ok, attachment} <- Attachments.fetch(context(), id),
         :ok <- readable_file(attachment.managed_path) do
      conn
      |> put_resp_content_type(attachment.media_type)
      |> put_resp_header("content-disposition", "attachment")
      |> send_file(200, attachment.managed_path)
    else
      {:error, error} -> JSON.error(conn, error)
    end
  end

  get "/api/v1/events" do
    EventsStream.stream(conn)
  end

  get "/health" do
    case JSON.health() do
      :ok -> JSON.send(conn, 200, %{"status" => "ok"})
      {:error, _reason} -> JSON.send(conn, 503, %{"status" => "unavailable"})
      _other -> JSON.send(conn, 503, %{"status" => "unavailable"})
    end
  end

  match _ do
    cond do
      String.starts_with?(conn.request_path, "/api/") ->
        JSON.error(conn, %Error{kind: :not_found, message: "route not found"})

      browser_route?(conn.request_path) ->
        SPA.send_index(conn)

      true ->
        send_resp(conn, 404, "not found")
    end
  end

  defp ticket_list(conn, {:ok, tickets}),
    do: JSON.send(conn, 200, %{"tickets" => Enum.map(tickets, &Presenter.ticket_summary/1)})

  defp ticket_list(conn, {:error, error}), do: JSON.error(conn, error)
  defp context, do: Context.global(:me)

  defp valid_project_key(key) when is_binary(key) do
    if Regex.match?(@project_key, key),
      do: :ok,
      else: validation_error(:project_key, "must be a valid project key")
  end

  defp valid_project_key(_), do: validation_error(:project_key, "must be a valid project key")

  defp valid_ticket_identifier(identifier) when is_binary(identifier) do
    if Regex.match?(@ticket_identifier, identifier),
      do: :ok,
      else: validation_error(:identifier, "must be a valid ticket identifier")
  end

  defp valid_ticket_identifier(_),
    do: validation_error(:identifier, "must be a valid ticket identifier")

  defp valid_uuid(value) do
    case Ecto.UUID.cast(value) do
      {:ok, id} -> {:ok, id}
      :error -> validation_error(:id, "must be a valid UUID")
    end
  end

  defp readable_file(path) do
    if File.regular?(path),
      do: :ok,
      else: {:error, %Error{kind: :not_found, message: "attachment not found"}}
  end

  defp validation_error(field, message),
    do:
      {:error,
       %Error{
         kind: :validation_failed,
         message: "request validation failed",
         fields: %{field => [message]}
       }}

  defp browser_route?(path) do
    not String.starts_with?(path, "/api/") and path != "/health" and
      not String.contains?(Path.basename(path), ".")
  end
end

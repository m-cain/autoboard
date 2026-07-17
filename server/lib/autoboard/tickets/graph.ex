defmodule Autoboard.Tickets.Graph do
  @moduledoc false

  @spec reachable?([{term(), term()}], term(), term()) :: boolean()
  def reachable?(edges, from, target) when is_list(edges) do
    adjacency =
      Enum.reduce(edges, %{}, fn
        {source, destination}, graph ->
          Map.update(graph, source, [destination], &[destination | &1])

        _edge, graph ->
          graph
      end)

    traverse([from], MapSet.new(), target, adjacency)
  end

  def reachable?(_edges, _from, _target), do: false

  defp traverse([], _seen, _target, _adjacency), do: false

  defp traverse([target | _rest], _seen, target, _adjacency), do: true

  defp traverse([current | rest], seen, target, adjacency) do
    if MapSet.member?(seen, current) do
      traverse(rest, seen, target, adjacency)
    else
      neighbors = Map.get(adjacency, current, [])
      traverse(neighbors ++ rest, MapSet.put(seen, current), target, adjacency)
    end
  end
end

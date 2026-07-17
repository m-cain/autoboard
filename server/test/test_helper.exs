ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(Autoboard.Repo, :manual)

ExUnit.after_suite(fn _results ->
  path = Application.fetch_env!(:autoboard, :socket_path)

  if Process.whereis(Autoboard.RPC.Listener), do: GenServer.stop(Autoboard.RPC.Listener)

  _ = Application.stop(:autoboard)

  wait_for_removal = fn
    _wait_for_removal, 0 ->
      not File.exists?(path)

    wait_for_removal, attempts ->
      if File.exists?(path),
        do:
          (
            Process.sleep(10)
            wait_for_removal.(wait_for_removal, attempts - 1)
          ),
        else: true
  end

  unless wait_for_removal.(wait_for_removal, 20) do
    raise("test socket residue: #{path}")
  end
end)

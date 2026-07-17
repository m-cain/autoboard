ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(Autoboard.Repo, :manual)

ExUnit.after_suite(fn _results ->
  path = Application.fetch_env!(:autoboard, :socket_path)
  data_dir = Application.fetch_env!(:autoboard, :data_dir)

  if Process.whereis(Autoboard.RPC.Listener), do: GenServer.stop(Autoboard.RPC.Listener)

  _ = Application.stop(:autoboard)

  wait_for_removal = fn
    _wait_for_removal, 0 ->
      artifacts =
        [path, path <> ".owner", path <> ".owner.claim"] ++ Path.wildcard(path <> ".owner.tmp-*")

      Enum.all?(artifacts, &(not File.exists?(&1)))

    wait_for_removal, attempts ->
      artifacts =
        [path, path <> ".owner", path <> ".owner.claim"] ++ Path.wildcard(path <> ".owner.tmp-*")

      if Enum.any?(artifacts, &File.exists?/1),
        do:
          (
            Process.sleep(10)
            wait_for_removal.(wait_for_removal, attempts - 1)
          ),
        else: true
  end

  unless wait_for_removal.(wait_for_removal, 20) do
    raise("test listener residue: #{path}")
  end

  expected_prefix = Path.join(System.tmp_dir!(), "autoboard-test-")

  unless String.starts_with?(data_dir, expected_prefix) and Path.dirname(path) == data_dir do
    raise("refusing to remove non-suite test data directory: #{data_dir}")
  end

  File.rm_rf!(data_dir)

  if File.exists?(data_dir) do
    raise("test data directory residue: #{data_dir}")
  end
end)

defmodule Autoboard.MixProject do
  use Mix.Project

  def project do
    [
      app: :autoboard,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  # Run "mix help compile.app" to learn about applications.
  def application do
    [
      extra_applications: [:logger],
      mod: {Autoboard.Application, []}
    ]
  end

  # Run "mix help deps" to learn about dependencies.
  defp deps do
    [
      {:ecto_sql, "~> 3.14"},
      {:postgrex, "~> 0.22.3"},
      {:plug, "~> 1.20"},
      {:bandit, "~> 1.12"},
      {:jason, "~> 1.4"},
      {:xema, "~> 0.17.9", only: :test}
    ]
  end
end

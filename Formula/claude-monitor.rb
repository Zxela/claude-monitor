class ClaudeMonitor < Formula
  desc "Real-time observability dashboard for Claude Code sessions"
  homepage "https://github.com/Zxela/claude-monitor"
  version "3.8.2"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-amd64.tar.gz"
      sha256 "c87becd070a182c3a4bf3a1969a7dc1b32c1e756f91f64e766698ed63a9ce1d7"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-arm64.tar.gz"
      sha256 "f4f914ce09e77a1f8e498cdb6592ca5344e113f176dafbfcc332e56792e34265"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-amd64.tar.gz"
      sha256 "0f3170e5c60850ab79accc77968528b28b6370311d95da073a9dfcdd3443d421"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-arm64.tar.gz"
      sha256 "39c53300ee76ad9802f6b8fd0ba0313cb72d2f8295c94ad68a545079f711ca02"
    end
  end

  def install
    bin.install "claude-monitor"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/claude-monitor --version")
  end

  service do
    run [opt_bin/"claude-monitor"]
    keep_alive true
    log_path var/"log/claude-monitor.log"
    error_log_path var/"log/claude-monitor.log"
  end
end

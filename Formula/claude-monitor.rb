class ClaudeMonitor < Formula
  desc "Real-time observability dashboard for Claude Code sessions"
  homepage "https://github.com/Zxela/claude-monitor"
  version "3.1.2"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-amd64.tar.gz"
      sha256 "8b7ed18cae5f332515579f6748ac45c10a0dca81e4ce90b6cabd410f225e39c3"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-arm64.tar.gz"
      sha256 "7234611ea5bd2a40b7d52ce2fa683c75fb66b33ca1582cb622b8dcf88d3d11c7"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-amd64.tar.gz"
      sha256 "f656d3c0661013e02e5d8bcf8522566a61cc21494511f3fcd3af950a5280f87e"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-arm64.tar.gz"
      sha256 "3d5b280d52ea0174f00c7de89320b23b3016d8734204eea828376445a299146c"
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

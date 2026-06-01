class ClaudeMonitor < Formula
  desc "Real-time observability dashboard for Claude Code sessions"
  homepage "https://github.com/Zxela/claude-monitor"
  version "3.6.1"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-amd64.tar.gz"
      sha256 "9cd7ed8f8a683cca65608dd02ddfff8df51240acafe17e4de5f84589d78c9c69"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-arm64.tar.gz"
      sha256 "6c96943ea13d4f8656a3b8c09ebb97a7988550ef7d5969719d41dace0ee0d9c9"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-amd64.tar.gz"
      sha256 "73113545ba16284bf88b9036eff4d64d9cd6b3b5c761758b99574d6cc9bd900b"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-arm64.tar.gz"
      sha256 "5aa466a7506a1fa0cd0d60e32181e8d2bedeb660af98d17f4440a5e88b74a22b"
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

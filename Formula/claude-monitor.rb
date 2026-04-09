class ClaudeMonitor < Formula
  desc "Real-time observability dashboard for Claude Code sessions"
  homepage "https://github.com/Zxela/claude-monitor"
  version "3.1.0"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-amd64.tar.gz"
      sha256 "0d9f09ae3678a9745ff86a9c1d698a5175a89eca0af92263a899f448f2747584"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-arm64.tar.gz"
      sha256 "46e84793d5b393f94e7f7091b3b287ccc8f0d5a36d1e8f71185ad136bb2761ed"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-amd64.tar.gz"
      sha256 "dab3ef17fc7a66a473b028ff9e37716a74fe54c6951a7e30e7990d91ecad7256"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-arm64.tar.gz"
      sha256 "347ad58f0d70ba239fae13d00f38252b1af3eba8feae5ba543554d8d9b661bce"
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

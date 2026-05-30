class ClaudeMonitor < Formula
  desc "Real-time observability dashboard for Claude Code sessions"
  homepage "https://github.com/Zxela/claude-monitor"
  version "3.5.1"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-amd64.tar.gz"
      sha256 "1a3c67ad386f4c65e5b56cf3a7c5739ec691db9665a4015374492eb6b188bca6"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-darwin-arm64.tar.gz"
      sha256 "34aceb307f647237cf74dfcd10f5ffaba52e7aeeaea36f9988577c376ebf4765"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-amd64.tar.gz"
      sha256 "93e64cf20a9fd66cab3dc11fdf6f9ef1ace2c741918f2dbfb80e5f1268ac0703"
    end

    on_arm do
      url "https://github.com/Zxela/claude-monitor/releases/download/v#{version}/claude-monitor-linux-arm64.tar.gz"
      sha256 "466409c6ff63e665542b3908a14168b29792755c023f9a112dd7fc4f23a38517"
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

#!/bin/bash
# Kaitu Node Installation Script
# Usage: curl -fsSL https://k2.52j.me/slave/init-node.sh | sudo bash
#
# This script:
# 1. Sets up system directories and basic packages
# 2. Creates ubuntu user with SSH key authentication
# 3. Installs Docker and Docker Compose
# 4. Deploys kaitu-slave service (auto-generates node secret)

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Constants
CENTER_URL="https://k2.52j.me"
SSH_PORT="1022"
TIMEZONE="Asia/Singapore"
APPS_DIR="/apps"
KAITU_DIR="/apps/kaitu-slave"
LOGS_DIR="/apps/logs"
LOG_FILE="/tmp/kaitu-install-$(date +%Y%m%d-%H%M%S).log"

# Pinned versions - all nodes MUST use identical versions
DOCKER_VERSION="27.4.1"
CONTAINERD_VERSION="1.7.24"
COMPOSE_VERSION="2.32.4"

# Logging functions - output to both console and log file
log_raw()  { echo -e "$1" | tee -a "$LOG_FILE"; }
info()     { log_raw "${BLUE}[INFO]${NC} $1"; }
success()  { log_raw "${GREEN}[OK]${NC} $1"; }
warn()     { log_raw "${YELLOW}[WARN]${NC} $1"; }
error()    { log_raw "${RED}[ERROR]${NC} $1"; echo "Log file: $LOG_FILE"; exit 1; }

# Run command with logging (shows output on failure)
run_cmd() {
    local desc="$1"
    shift
    info "$desc"
    if ! "$@" >> "$LOG_FILE" 2>&1; then
        error "Failed: $desc (see $LOG_FILE for details)"
    fi
}

# Run command with full output visible
run_verbose() {
    local desc="$1"
    shift
    info "$desc"
    if ! "$@" 2>&1 | tee -a "$LOG_FILE"; then
        error "Failed: $desc"
    fi
}

check_root() {
    [[ $EUID -ne 0 ]] && error "This script must be run as root"
    success "Running as root"
}

detect_os() {
    [[ -f /etc/os-release ]] && . /etc/os-release || error "Cannot detect OS"
    OS=$ID
    VERSION=$VERSION_ID
    success "Detected OS: $OS $VERSION"
}

setup_directories() {
    info "Creating directories..."
    mkdir -p "$APPS_DIR" "$LOGS_DIR" "$KAITU_DIR"
    success "Directories created"
}

# Calculate swap size based on RAM
# RAM < 2GB: swap = RAM
# RAM 2-8GB: swap = RAM/2
# RAM > 8GB: swap = 4GB
calculate_swap_size() {
    local mem_mb=$(free -m | awk '/^Mem:/{print $2}')
    local swap_mb

    if [[ $mem_mb -lt 2048 ]]; then
        swap_mb=$mem_mb
    elif [[ $mem_mb -lt 8192 ]]; then
        swap_mb=$((mem_mb / 2))
    else
        swap_mb=4096
    fi

    echo $swap_mb
}

setup_swap() {
    # Skip if swap already exists
    if [[ $(swapon --show | wc -l) -gt 0 ]]; then
        local current_swap=$(free -m | awk '/^Swap:/{print $2}')
        success "Swap already configured: ${current_swap}MB"
        return
    fi

    local swap_mb=$(calculate_swap_size)
    local swap_file="/swapfile"

    info "Creating ${swap_mb}MB swap..."

    # Create swap file
    dd if=/dev/zero of="$swap_file" bs=1M count="$swap_mb" >> "$LOG_FILE" 2>&1
    chmod 600 "$swap_file"
    mkswap "$swap_file" >> "$LOG_FILE" 2>&1
    swapon "$swap_file"

    # Make permanent
    if ! grep -q "$swap_file" /etc/fstab; then
        echo "$swap_file none swap sw 0 0" >> /etc/fstab
    fi

    # Optimize swap settings
    echo "vm.swappiness=10" > /etc/sysctl.d/99-swap.conf
    sysctl -p /etc/sysctl.d/99-swap.conf >> "$LOG_FILE" 2>&1

    success "Swap configured: ${swap_mb}MB"
}

cleanup_system() {
    info "Cleaning up system..."

    # Remove snapd (wastes space and resources)
    if dpkg -l snapd &>/dev/null; then
        info "Removing snapd..."
        systemctl stop snapd.socket snapd >> "$LOG_FILE" 2>&1 || true
        apt-get remove -y --purge snapd >> "$LOG_FILE" 2>&1 || true
        rm -rf /snap /var/snap /var/lib/snapd /var/cache/snapd >> "$LOG_FILE" 2>&1 || true
        # Prevent reinstall
        cat > /etc/apt/preferences.d/nosnap.pref << 'SNAPEOF'
Package: snapd
Pin: release a=*
Pin-Priority: -10
SNAPEOF
        success "snapd removed"
    fi

    # Remove docker.io (conflicts with docker-ce)
    if dpkg -l docker.io &>/dev/null || dpkg -l docker-doc &>/dev/null || dpkg -l podman-docker &>/dev/null; then
        info "Removing conflicting Docker packages (docker.io, podman)..."
        systemctl stop docker >> "$LOG_FILE" 2>&1 || true
        apt-get remove -y --purge docker.io docker-doc docker-compose podman-docker containerd runc >> "$LOG_FILE" 2>&1 || true
        rm -rf /var/lib/docker /var/lib/containerd >> "$LOG_FILE" 2>&1 || true
        success "Conflicting packages removed"
    fi

    # Remove nginx (conflicts with port 443)
    if dpkg -l nginx &>/dev/null; then
        info "Removing nginx (conflicts with port 443)..."
        systemctl stop nginx >> "$LOG_FILE" 2>&1 || true
        apt-get remove -y --purge nginx nginx-common nginx-full >> "$LOG_FILE" 2>&1 || true
        success "nginx removed"
    fi

    # Clean up
    apt-get autoremove -y >> "$LOG_FILE" 2>&1 || true
    apt-get clean >> "$LOG_FILE" 2>&1 || true

    success "System cleanup complete"
}

install_packages() {
    info "Installing packages..."
    apt-get update >> "$LOG_FILE" 2>&1
    apt-get install -y curl wget ca-certificates gnupg lsb-release htop vim git unzip jq iptables >> "$LOG_FILE" 2>&1
    success "Packages installed"
}

setup_timezone() {
    info "Setting timezone to $TIMEZONE..."
    timedatectl set-timezone "$TIMEZONE" 2>/dev/null || ln -sf "/usr/share/zoneinfo/$TIMEZONE" /etc/localtime
    success "Timezone set"
}

setup_ssh() {
    info "Configuring SSH on port $SSH_PORT..."
    [[ ! -f /etc/ssh/sshd_config.bak ]] && cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak
    sed -i "s/^#*Port .*/Port $SSH_PORT/" /etc/ssh/sshd_config
    sed -i 's/^#*PubkeyAuthentication .*/PubkeyAuthentication yes/' /etc/ssh/sshd_config
    systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null || true
    success "SSH configured on port $SSH_PORT"
}

setup_user() {
    info "Setting up ubuntu user..."

    # Create ubuntu user if not exists
    if ! id "ubuntu" &>/dev/null; then
        useradd -m -s /bin/bash ubuntu
        success "Created ubuntu user"
    else
        success "ubuntu user already exists"
    fi

    # Setup sudo access
    echo "ubuntu ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/ubuntu
    chmod 440 /etc/sudoers.d/ubuntu

    # Setup SSH directory
    mkdir -p /home/ubuntu/.ssh
    chmod 700 /home/ubuntu/.ssh
    chown ubuntu:ubuntu /home/ubuntu/.ssh

    # Fetch SSH public key from center
    info "Fetching SSH public key from ${CENTER_URL}/slave/ssh-pubkey..."
    local ssh_url="${CENTER_URL}/slave/ssh-pubkey"
    local ssh_response

    if ! ssh_response=$(curl -fsSL "$ssh_url" 2>&1); then
        echo "curl failed: $ssh_response" >> "$LOG_FILE"
        error "Failed to fetch SSH public key from $ssh_url"
    fi

    # Validate SSH key format (should start with ssh-ed25519 or ssh-rsa)
    if [[ ! "$ssh_response" =~ ^ssh-(ed25519|rsa|ecdsa) ]]; then
        echo "Invalid SSH key format: $ssh_response" >> "$LOG_FILE"
        error "Invalid SSH public key format received"
    fi

    # Install the key
    echo "$ssh_response" > /home/ubuntu/.ssh/authorized_keys
    chmod 600 /home/ubuntu/.ssh/authorized_keys
    chown ubuntu:ubuntu /home/ubuntu/.ssh/authorized_keys

    # Verify
    local key_type=$(echo "$ssh_response" | awk '{print $1}')
    success "SSH key installed ($key_type)"
    echo "SSH public key: ${ssh_response:0:50}..." >> "$LOG_FILE"
}

check_docker_version() {
    # Check if docker is installed and matches required version
    if ! command -v docker &> /dev/null; then
        return 1  # Not installed
    fi

    local current_version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "")
    local current_compose=$(docker compose version --short 2>/dev/null || echo "")

    if [[ "$current_version" != "$DOCKER_VERSION" ]]; then
        warn "Docker version mismatch: $current_version (need $DOCKER_VERSION)"
        return 1
    fi

    if [[ "$current_compose" != "$COMPOSE_VERSION" ]]; then
        warn "Compose version mismatch: $current_compose (need $COMPOSE_VERSION)"
        return 1
    fi

    return 0
}

install_docker() {
    local need_install=true

    # Check if correct version already installed
    if check_docker_version; then
        success "Docker $DOCKER_VERSION already installed"
        need_install=false
    fi

    if [[ "$need_install" == "false" ]]; then
        # Ensure ubuntu user is in docker group even if Docker was pre-installed
        if ! groups ubuntu 2>/dev/null | grep -q docker; then
            info "Adding ubuntu to docker group..."
            usermod -aG docker ubuntu
        fi
        show_docker_version
        return
    fi

    info "Installing Docker CE $DOCKER_VERSION (pinned version)..."

    # Stop existing docker if running
    systemctl stop docker >> "$LOG_FILE" 2>&1 || true

    # Remove any existing docker packages (wrong version)
    if command -v docker &> /dev/null; then
        info "Removing existing Docker (wrong version)..."
        apt-get remove -y --purge docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >> "$LOG_FILE" 2>&1 || true
    fi

    # Fix iptables/nftables compatibility (Ubuntu 22.04+)
    # CRITICAL: Must switch to iptables-legacy AND flush nftables rules
    # Otherwise Docker may have rules in nftables that conflict with new iptables-legacy rules
    if command -v update-alternatives &> /dev/null; then
        info "Switching to iptables-legacy for Docker compatibility..."
        update-alternatives --set iptables /usr/sbin/iptables-legacy 2>/dev/null || true
        update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy 2>/dev/null || true

        # Flush nftables rules to avoid conflict with iptables-legacy
        # Docker will recreate all rules using iptables-legacy on restart
        if command -v nft &> /dev/null; then
            nft flush ruleset >> "$LOG_FILE" 2>&1 || true
        fi
    fi

    # Add Docker repository
    info "Adding Docker repository..."
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/$OS/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc

    local codename=$(. /etc/os-release && echo "$VERSION_CODENAME")
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$OS $codename stable" | tee /etc/apt/sources.list.d/docker.list >> "$LOG_FILE"

    apt-get update >> "$LOG_FILE" 2>&1

    # Build version strings for apt
    # Format: 5:27.4.1-1~ubuntu.24.04~noble
    local docker_pkg="5:${DOCKER_VERSION}-1~${OS}.${VERSION_ID}~${codename}"
    local containerd_pkg="${CONTAINERD_VERSION}-1"
    local compose_pkg="${COMPOSE_VERSION}-1~${OS}.${VERSION_ID}~${codename}"

    info "Installing pinned versions..."
    info "  docker-ce: $docker_pkg"
    info "  containerd.io: $containerd_pkg"
    info "  docker-compose-plugin: $compose_pkg"

    # Install with specific versions
    if ! apt-get install -y \
        "docker-ce=$docker_pkg" \
        "docker-ce-cli=$docker_pkg" \
        "containerd.io=$containerd_pkg" \
        docker-buildx-plugin \
        "docker-compose-plugin=$compose_pkg" >> "$LOG_FILE" 2>&1; then

        # Fallback: try without pinned versions (for new distros)
        warn "Pinned version not available, installing latest..."
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >> "$LOG_FILE" 2>&1
    fi

    # Hold packages to prevent auto-upgrade
    apt-mark hold docker-ce docker-ce-cli containerd.io docker-compose-plugin >> "$LOG_FILE" 2>&1

    # Disable UFW if enabled
    if command -v ufw &> /dev/null && ufw status | grep -q "active"; then
        warn "Disabling UFW (Docker bypasses it, use iptables directly)"
        ufw disable >> "$LOG_FILE" 2>&1
    fi

    systemctl start docker
    systemctl enable docker >> "$LOG_FILE" 2>&1
    usermod -aG docker ubuntu

    # Verify installation
    if ! check_docker_version; then
        warn "Installed Docker version differs from pinned version (may be newer distro)"
    fi

    show_docker_version
}

show_docker_version() {
    local docker_ver=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'unknown')
    local compose_ver=$(docker compose version --short 2>/dev/null || echo 'unknown')
    local iptables_ver=$(iptables --version 2>/dev/null | awk '{print $2}' || echo 'unknown')

    echo ""
    info "Docker Version Info:"
    echo "  Engine:   $docker_ver (pinned: $DOCKER_VERSION)"
    echo "  Compose:  $compose_ver (pinned: $COMPOSE_VERSION)"
    echo "  iptables: $iptables_ver"

    # Log to file
    echo "Docker: $docker_ver, Compose: $compose_ver, iptables: $iptables_ver" >> "$LOG_FILE"
    echo ""
}

setup_kaitu_service() {
    info "Setting up kaitu-slave..."
    cd "$KAITU_DIR"

    # Clean up any existing containers to avoid port conflicts
    if command -v docker &> /dev/null; then
        info "Cleaning up existing containers..."
        docker compose down >> "$LOG_FILE" 2>&1 || true
        docker rm -f k2-slave k2-slave-sidecar k2-oc >> "$LOG_FILE" 2>&1 || true
    fi

    # Download docker-compose.yml
    info "Downloading docker-compose.yml..."
    if ! curl -fsSL "${CENTER_URL}/slave/docker-compose.yml" -o docker-compose.yml 2>> "$LOG_FILE"; then
        error "Failed to download docker-compose.yml from ${CENTER_URL}/slave/docker-compose.yml"
    fi
    [[ ! -f docker-compose.yml ]] && error "docker-compose.yml not found after download"

    # Generate random secret
    NODE_SECRET=$(openssl rand -hex 32)

    # Generate .env
    cat > .env << EOF
# Kaitu Slave Node Configuration
# Generated on $(date -Iseconds)

K2_NODE_SECRET=${NODE_SECRET}
K2_CENTER_URL=${CENTER_URL}
K2_PORT=443
K2OC_ENABLED=false
K2OC_PORT=10001
K2_TEST_NODE=false
K2_HAS_RELAY=false
K2_JUMP_PORT_MIN=10020
K2_JUMP_PORT_MAX=10119
REPORT_INTERVAL=120s
K2_LOG_LEVEL=info
K2_LOG_TO_FILE=false
K2_LOG_DIR=./logs
EOF

    success ".env created with auto-generated secret"

    # Start services
    info "Pulling Docker images..."
    docker compose pull 2>&1 | tee -a "$LOG_FILE" || docker-compose pull 2>&1 | tee -a "$LOG_FILE"

    info "Starting services..."
    docker compose up -d 2>&1 | tee -a "$LOG_FILE" || docker-compose up -d 2>&1 | tee -a "$LOG_FILE"

    # Show container status
    info "Container status:"
    docker compose ps 2>&1 | tee -a "$LOG_FILE" || docker-compose ps 2>&1 | tee -a "$LOG_FILE"
    success "Services started"
}

print_summary() {
    echo ""
    echo "=============================================="
    echo -e "${GREEN}Kaitu Node Installation Complete!${NC}"
    echo "=============================================="
    echo ""
    echo "SSH Access:"
    echo "  User: ubuntu"
    echo "  Port: ${SSH_PORT}"
    echo ""
    echo "Service: ${KAITU_DIR}"
    echo ""
    echo "Commands:"
    echo "  cd ${KAITU_DIR}"
    echo "  docker compose logs -f"
    echo "  docker compose restart"
    echo ""
    echo "Install Log: ${LOG_FILE}"
    echo "=============================================="
}

main() {
    echo ""
    echo "=============================================="
    echo "    Kaitu Node Installation Script"
    echo "=============================================="
    echo ""
    echo "Log file: $LOG_FILE"
    echo ""

    # Initialize log file
    echo "=== Kaitu Node Installation ===" > "$LOG_FILE"
    echo "Started: $(date)" >> "$LOG_FILE"
    echo "================================" >> "$LOG_FILE"

    check_root
    detect_os
    setup_directories
    cleanup_system      # Remove snapd, docker.io, nginx
    setup_swap          # Configure swap based on RAM
    install_packages
    setup_timezone
    setup_ssh
    setup_user
    install_docker      # Install pinned Docker version
    setup_kaitu_service
    print_summary
}

main

# Deployment Guide

## Option 1: Railway.app (Easiest - 5 minutes)

**Pros**: Free tier, automatic deployments from GitHub, easy setup
**Cons**: Limited to 500 hours/month on free tier

### Steps:

1. Go to https://railway.app/
2. Sign up with GitHub
3. Click "New Project" → "Deploy from GitHub repo"
4. Select `iavorskyi/no-gap-gas-v2`
5. Railway will auto-detect the Dockerfile and build
6. Add environment variables:
   - Click on your service → "Variables" tab
   - Add each variable from your `.env` file:
     ```
     GASOLINA_EMAIL=7038329@ukr.net
     GASOLINA_PASSWORD=your-password
     GASOLINA_CHECK_URL=https://gasolina-online.com/indicator
     GASOLINA_MONTHLY_INCREMENTS={"1":110,"2":100,"3":50,"4":30,"5":15,"6":15,"7":15,"8":15,"9":15,"10":50,"11":70,"12":100}
     CRON_SCHEDULE=0 0 1 * *
     ```
7. Click "Deploy" - it will run 24/7 and execute on schedule

**View logs**: Click on your service → "Deployments" → "View Logs"

---

## Option 2: Render.com (Also Easy - Free Tier)

**Pros**: Free tier, easy deployment, good for background workers
**Cons**: Spins down after 15 min of inactivity (but will wake up on schedule)

### Steps:

1. Go to https://render.com/
2. Sign up with GitHub
3. Click "New +" → "Background Worker"
4. Connect your GitHub repo: `iavorskyi/no-gap-gas-v2`
5. Configure:
   - **Name**: no-gap-gas-v2
   - **Environment**: Docker
   - **Docker Command**: `./no-gap-gas-v2`
   - **Plan**: Free
6. Add environment variables (same as Railway)
7. Click "Create Background Worker"

---

## Option 3: DigitalOcean Droplet ($4-6/month - Most Reliable)

**Pros**: Always running, full control, most reliable for scheduled tasks
**Cons**: Costs money (~$4-6/month)

### Steps:

1. Create a droplet on https://www.digitalocean.com/
   - Choose Ubuntu 22.04
   - Basic plan ($4-6/month)
   - Choose datacenter region close to you

2. SSH into your droplet:
   ```bash
   ssh root@your-droplet-ip
   ```

3. Install Docker:
   ```bash
   curl -fsSL https://get.docker.com -o get-docker.sh
   sh get-docker.sh
   ```

4. Clone your repository:
   ```bash
   git clone https://github.com/iavorskyi/no-gap-gas-v2.git
   cd no-gap-gas-v2
   ```

5. Create `.env` file:
   ```bash
   nano .env
   # Paste your environment variables
   # Save with Ctrl+X, Y, Enter
   ```

6. Build and run with Docker:
   ```bash
   docker build -t gasolina-automation .
   docker run -d --name gasolina --env-file .env --restart unless-stopped gasolina-automation
   ```

7. Check logs:
   ```bash
   docker logs -f gasolina
   ```

8. To update later:
   ```bash
   cd no-gap-gas-v2
   git pull
   docker stop gasolina
   docker rm gasolina
   docker build -t gasolina-automation .
   docker run -d --name gasolina --env-file .env --restart unless-stopped gasolina-automation
   ```

---

## Option 4: Fly.io (Advanced - Free Tier)

**Pros**: Free tier, global deployment, good performance
**Cons**: Requires CLI tool installation

### Steps:

1. Install flyctl:
   ```bash
   curl -L https://fly.io/install.sh | sh
   ```

2. Sign up and login:
   ```bash
   fly auth signup
   # or
   fly auth login
   ```

3. Launch the app:
   ```bash
   fly launch --no-deploy
   # Choose app name: no-gap-gas-v2
   # Choose region: Amsterdam (or closest to you)
   ```

4. Set secrets:
   ```bash
   fly secrets set GASOLINA_EMAIL="7038329@ukr.net"
   fly secrets set GASOLINA_PASSWORD="your-password"
   fly secrets set GASOLINA_CHECK_URL="https://gasolina-online.com/indicator"
   fly secrets set GASOLINA_MONTHLY_INCREMENTS='{"1":110,"2":100,"3":50,"4":30,"5":15,"6":15,"7":15,"8":15,"9":15,"10":50,"11":70,"12":100}'
   fly secrets set CRON_SCHEDULE="0 0 1 * *"
   ```

5. Deploy:
   ```bash
   fly deploy
   ```

6. Check logs:
   ```bash
   fly logs
   ```

---

## Option 5: VPS with systemd (Any Linux VPS)

**Pros**: Full control, works with any VPS provider
**Cons**: Requires more setup

### Steps:

1. SSH into your VPS
2. Install Go:
   ```bash
   wget https://go.dev/dl/go1.24.linux-amd64.tar.gz
   sudo tar -C /usr/local -xzf go1.24.linux-amd64.tar.gz
   export PATH=$PATH:/usr/local/go/bin
   ```

3. Install Chrome:
   ```bash
   wget -q -O - https://dl-ssl.google.com/linux/linux_signing_key.pub | sudo apt-key add -
   sudo sh -c 'echo "deb [arch=amd64] http://dl.google.com/linux/chrome/deb/ stable main" >> /etc/apt/sources.list.d/google-chrome.list'
   sudo apt update
   sudo apt install -y google-chrome-stable
   ```

4. Clone and build:
   ```bash
   git clone https://github.com/iavorskyi/no-gap-gas-v2.git
   cd no-gap-gas-v2
   go build
   ```

5. Create `.env` file with your credentials

6. Create systemd service:
   ```bash
   sudo nano /etc/systemd/system/gasolina.service
   ```

   Paste:
   ```ini
   [Unit]
   Description=Gasolina Online Automation
   After=network.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/root/no-gap-gas-v2
   ExecStart=/root/no-gap-gas-v2/my-go-service
   Restart=always
   RestartSec=10

   [Install]
   WantedBy=multi-user.target
   ```

7. Start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable gasolina
   sudo systemctl start gasolina
   sudo systemctl status gasolina
   ```

8. View logs:
   ```bash
   sudo journalctl -u gasolina -f
   ```

---

## Recommended Choice

**For easiest setup**: Use **Railway.app** (Option 1) - literally 5 minutes to deploy

**For most reliable**: Use **DigitalOcean Droplet** (Option 3) - $4-6/month but always running

**For free but reliable**: Use **Fly.io** (Option 4) - free tier is generous

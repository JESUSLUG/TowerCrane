# How to Create Private TowerCrane-Example Repository

This guide will help you create the private repository on GitHub with all the infrastructure templates.

## Step 1: Create Repository on GitHub

1. Go to https://github.com/new
2. Fill in the form:
   - **Repository name**: `TowerCrane-Example`
   - **Description**: `Private infrastructure repository for TowerCrane with OpenTofu and LocalStack`
   - **Visibility**: Select **Private** ✅
   - **Initialize with**: None (we'll push existing content)
3. Click "Create repository"

## Step 2: Copy Template Files Locally

All template files are in this session folder:
```
/home/eduardo_lugo/.copilot/session-state/0e6cbf5d-83bb-4b9f-9c68-6c72aff40815/files/example-repo-template/
```

Files to copy:
- `environments/dev/*` - Development environment
- `environments/prod/*` - Production environment
- `modules/networking/*` - Networking module
- `modules/compute/*` - Compute module
- `modules/database/*` - Database module
- `docker-compose.floci.yml` - LocalStack Docker setup
- `setup.sh` - Automated setup
- `README.md` - Documentation
- `.gitignore` - Git ignore patterns

## Step 3: Clone and Push to GitHub

```bash
# Create directory and initialize git
mkdir TowerCrane-Example
cd TowerCrane-Example
git init
git config user.email "dev@towercrane.io"
git config user.name "TowerCrane Team"

# Create the directory structure
mkdir -p environments/{dev,prod}
mkdir -p modules/{networking,compute,database}

# Copy all template files from session folder to here
# (You'll see the paths above)

# Add all files
git add .

# Create initial commit
git commit -m "Initial infrastructure setup with OpenTofu and Floci

- Multi-environment configuration (dev/prod)
- Reusable infrastructure modules
- LocalStack integration for AWS emulation
- Ready for TowerCrane GitOps workflow"

# Add GitHub remote and push
git remote add origin https://github.com/JESUSLUG/TowerCrane-Example.git
git branch -M main
git push -u origin main
```

## Step 4: Verify Private Access

1. Go to https://github.com/JESUSLUG/TowerCrane-Example
2. It should show as **Private** ✅
3. Only you (and organization members you grant access) can see it

## Step 5: Connect to TowerCrane

Once the repo is created:

1. Start TowerCrane:
   ```bash
   cd TowerCrane
   docker-compose up
   # Frontend: http://localhost:3000
   # Backend: http://localhost:8080
   ```

2. Log in with your GitHub account

3. Go to **Repositories** section

4. Click **Connect Repository**

5. Select **TowerCrane-Example** from your available repos

6. TowerCrane will sync the repo data

## Step 6: Test with LocalStack/Floci

```bash
cd TowerCrane-Example

# Start LocalStack
docker-compose -f docker-compose.floci.yml up -d

# Initialize OpenTofu
cd environments/dev
tofu init

# Or use the setup script
./setup.sh
```

## Step 7: Create a Project in TowerCrane

1. In TowerCrane dashboard, go to **Projects**
2. Create a new project:
   - **Name**: Dev Infrastructure
   - **Repository**: TowerCrane-Example
   - **Path**: environments/dev
3. Save

4. TowerCrane will detect OpenTofu files and offer:
   - `tofu plan`
   - `tofu apply`
   - Operations through the UI

## File Structure

After setup, your repo structure should be:

```
TowerCrane-Example/
├── environments/
│   ├── dev/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   ├── terraform.tfvars
│   │   └── outputs.tf
│   └── prod/
│       ├── main.tf
│       ├── variables.tf
│       ├── terraform.tfvars
│       └── outputs.tf
├── modules/
│   ├── networking/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── outputs.tf
│   ├── compute/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── outputs.tf
│   └── database/
│       ├── main.tf
│       ├── variables.tf
│       └── outputs.tf
├── docker-compose.floci.yml
├── setup.sh
├── .gitignore
└── README.md
```

## ⚠️ Important Security Notes

✅ The repo is **PRIVATE** - only you can access it
✅ Dummy credentials in `terraform.tfvars` are safe for development
❌ Never commit real AWS keys
❌ Use AWS Secrets Manager for production passwords

## Troubleshooting

### "Repository not found" in TowerCrane

Make sure:
1. The repo exists and is visible in your GitHub account
2. Your GitHub OAuth token has `repo` and `read:org` scopes
3. You're viewing as the correct organization member

### "Failed to sync branches"

1. Check GitHub API quota: https://api.github.com/rate_limit
2. Refresh the token by logging out and in again
3. Ensure TowerCrane backend has internet access

### LocalStack won't start

```bash
# Check Docker
docker ps

# Restart
docker-compose -f docker-compose.floci.yml restart

# Check logs
docker-compose -f docker-compose.floci.yml logs -f
```

## Next Steps

Once the private repo is created and connected:

1. ✅ Test infrastructure with LocalStack
2. ✅ Create OpenTofu plans through TowerCrane UI
3. ✅ Try applying changes
4. ✅ Monitor infrastructure state
5. ✅ Track changes via Git commits

This is now a complete GitOps setup for infrastructure!

---

**Happy GitOps! 🚀**

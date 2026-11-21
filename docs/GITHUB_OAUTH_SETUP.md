# GitHub OAuth2 Setup Guide

TowerCrane uses GitHub OAuth2 for authentication with organization membership validation.

## Steps to Configure GitHub OAuth

### 1. Create a GitHub OAuth Application

1. Go to **GitHub Settings** → **Developer settings** → **OAuth Apps** → **New OAuth App**
   - Or visit: https://github.com/settings/developers

2. Fill in the form:
   - **Application name**: TowerCrane
   - **Homepage URL**: `http://localhost:5173` (for development)
   - **Authorization callback URL**: `http://localhost:8080/api/v1/auth/callback`

3. Copy your:
   - **Client ID**
   - **Client Secret**

### 2. Configure Environment Variables

#### Backend (.env)
```env
GITHUB_CLIENT_ID=your-github-oauth-app-id
GITHUB_CLIENT_SECRET=your-github-oauth-app-secret
GITHUB_REDIRECT_URI=http://localhost:8080/api/v1/auth/callback
GITHUB_ORG=your-github-organization
FRONTEND_URL=http://localhost:5173
```

### 3. Organization Setup

For TowerCrane to validate organization membership:

1. Go to your GitHub organization settings
2. Ensure the OAuth app is authorized for the organization
3. Set `GITHUB_ORG` env variable to your organization name

Users must be members of this organization to access TowerCrane.

## Development Workflow

### Start the application

```bash
# Terminal 1: Backend
cd backend
cp .env.example .env
# Edit .env with your GitHub OAuth credentials
go run cmd/main.go

# Terminal 2: Frontend
cd frontend
npm install
npm run dev
```

### Test the flow

1. Open `http://localhost:5173`
2. Click "Sign in with GitHub"
3. You'll be redirected to GitHub
4. After authorization, you'll be redirected back with a JWT token
5. The token is stored in localStorage and used for subsequent API calls

## Production Deployment

For production, update the URLs:

```env
GITHUB_REDIRECT_URI=https://your-domain.com/api/v1/auth/callback
FRONTEND_URL=https://your-domain.com
```

## Security Notes

- **JWT_SECRET**: Change this to a strong random string in production
- **Client Secret**: Never commit this to version control
- **HTTPS**: Always use HTTPS in production
- **Token Expiry**: JWT tokens expire after 24 hours
- **Organization Validation**: Automatically enforced on login

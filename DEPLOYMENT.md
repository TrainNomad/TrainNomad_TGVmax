# Guide de Déploiement TGVmax API

## Déploiement sur Render

### Étape 1 : Configuration du repository GitHub

```bash
# Assurer que le code est poussé sur main
git push origin main

# Créer les secrets GitHub Actions
gh secret set RENDER_TGVMAX_URL --body "https://your-app-name.onrender.com"
gh secret set RELOAD_SECRET --body "your-secure-secret-key"
```

### Étape 2 : Créer un nouveau service sur Render

1. Aller sur [render.com](https://render.com)
2. Cliquer sur "New" → "Web Service"
3. Connecter le repo GitHub
4. Configurer :
   - **Name**: `tgvmax-api`
   - **Runtime**: `Docker` (ou Go si disponible)
   - **Build Command**: `cd tgvmax && go run ./cmd -output data.bin.gz && go build -o tgvmax-api .`
   - **Start Command**: `cd tgvmax && ./tgvmax-api`
   - **Plan**: Free (512 MB, 0.1 CPU)

### Étape 3 : Ajouter les variables d'environnement

Dans les settings du service Render :

```
PORT=8000
RELOAD_SECRET=your-secure-secret-key
GOMEMORYALIMIT=350MiB
```

### Étape 4 : Configurer GitHub Actions

Le workflow `.github/workflows/update-tgvmax-data.yml` va :

1. **S'exécuter automatiquement** : Chaque jour à 3h UTC
2. **Télécharger** les données TGVmax de la SNCF
3. **Créer/mettre à jour** une Release GitHub avec `data.bin.gz`
4. **Recharger** les données en RAM sans redémarrage (POST `/reload`)

### Étape 5 : Tester le déploiement

```bash
# Health check
curl https://your-app-name.onrender.com/health

# Recherche
curl "https://your-app-name.onrender.com/api/tgvmax/search?from=PARIS&to=LYON"

# Reload manuel
curl -X POST \
  -H "X-Reload-Secret: your-secure-secret-key" \
  https://your-app-name.onrender.com/api/tgvmax/reload
```

---

## Architecture de déploiement

```
┌─────────────────────────────────────────────────────────────────────┐
│                        GitHub Actions (cron 3h UTC)                  │
│                                                                      │
│  1. Télécharger JSON SNCF                                           │
│  2. go run ./cmd -output data.bin.gz                                │
│  3. Créer Release avec data.bin.gz                                  │
│  4. POST /api/tgvmax/reload sur Render                              │
└──────────────────────────────────┬──────────────────────────────────┘
                                   │
                    ┌──────────────┴──────────────┐
                    ▼                             ▼
        ┌─────────────────────┐      ┌──────────────────────┐
        │  GitHub Release     │      │   Render Service     │
        │  latest-tgvmax      │      │  tgvmax-api          │
        │  (data.bin.gz)      │      │  (Go + Docker)       │
        └─────────────────────┘      │  512 MB RAM          │
                                     │  PORT=8000           │
                                     └──────────────────────┘
                                              ▲
                                     POST /api/tgvmax/reload
                                              │
                                     ┌────────┴────────┐
                                     │  Clients REST   │
                                     │  (Web, Mobile)  │
                                     └─────────────────┘
```

---

## Stratégies de chargement des données

### Option A : Depuis GitHub Release (recommandé)
- **Avantage** : Données versionnées, versioning Git
- **Déploiement** : Télécharger au démarrage depuis GitHub API
- **Mise à jour** : Reload sans redéploiement

```go
// main.go
if err := downloadDataFromRelease(); err != nil {
    // Fallback sur fichier local
    LoadFromGzip("data.bin.gz")
}
```

### Option B : Générer à chaque build
- **Avantage** : Données toujours fraîches
- **Déploiement** : `build_command` télécharge et compile
- **Inconvénient** : Lent (~2-5min par build)

```bash
# Procfile ou render.yaml
go run ./cmd -output data.bin.gz && ./tgvmax-api
```

### Option C : Data persist sur Render
- **Avantage** : Survit aux redéploiements
- **Configuration** : Utiliser un "Disk" sur Render
- **Sync** : POST `/reload` met à jour après Actions

```yaml
# render.yaml
disk:
  size: 1  # 1 GB
  mountPath: /app/data
```

---

## Maintenance

### Vérifier l'état du service

```bash
# Logs en temps réel
render logs tgvmax-api --follow

# Health check
curl https://your-app-name.onrender.com/health

# Statistiques
curl https://your-app-name.onrender.com/health | jq .
```

### Recharger les données manuellement

```bash
curl -X POST \
  -H "X-Reload-Secret: $(echo $RELOAD_SECRET)" \
  -H "Content-Type: application/json" \
  https://your-app-name.onrender.com/api/tgvmax/reload
```

### Déboguer les Actions

```bash
# Voir les logs du workflow
gh run list --workflow=update-tgvmax-data.yml

# Détail d'une exécution
gh run view <run-id> --log
```

### Déployer un fix urgent

```bash
# Créer une branche
git checkout -b hotfix/urgent-fix
git commit -am "Fix: ..."

# Merger
git push origin hotfix/urgent-fix
# PR/merge sur main

# Render redéploiera automatiquement
# (ou manuellement via API Render)
```

---

## Scaling & Limitations

| Paramètre | Free Tier Render | Recommandations |
|-----------|------------------|-----------------|
| Mémoire | 512 MB | Limiter à 350 MB (debug.SetMemoryLimit) |
| CPU | 0.1 shared | Requêtes HTTP seules (pas de calcul lourd) |
| Bande passante | Illimitée | - |
| Data transfer | - | - |
| Redéploiement auto | ✓ | À chaque push sur main |
| SSL/HTTPS | ✓ | Automatique |

### Pour passer à Pro/Standard

```bash
# Augmenter les ressources
render plans upgrade tgvmax-api --tier=standard
```

---

## Troubleshooting

### ❌ "Failed to download data"

**Symptôme** : 
```
Failed to connect to GitHub API
```

**Solutions** :
1. Vérifier `GITHUB_TOKEN` dans les secrets Actions
2. Vérifier la connectivité Internet (Render → GitHub)
3. Vérifier le quota GitHub API (60 req/h sans token)

### ❌ "Memory limit exceeded"

**Symptôme** :
```
fatal error: runtime: memory limit exceeded
```

**Solutions** :
1. Réduire la taille des données à charger
2. Filtrer les trajets dans `parseAndFilterData()`
3. Upgrade vers un plan payant avec plus de RAM

### ❌ "Reload endpoint returns 401"

**Symptôme** :
```
401 Unauthorized
```

**Solutions** :
1. Vérifier le header `X-Reload-Secret`
2. Vérifier la valeur dans Render "Environment Variables"
3. Vérifier le timing (le secret a peut-être changé)

### ❌ "Data.bin.gz not found at startup"

**Symptôme** :
```
Trying to download from GitHub...
```

**Solutions** :
1. Assurer que `data.bin.gz` est dans le repo
2. Ou créer une GitHub Release avec le fichier
3. Ou augmenter le timeout du build (60s)

### ❌ "Build timeout"

**Symptôme** :
```
Build failed: timeout after 45 minutes
```

**Solutions** :
1. Réduire la taille des données
2. Mettre en cache le fichier `data.bin.gz`
3. Générer les données localement → commit

---

## Bonnes pratiques

✅ **À faire** :
- Versioner `data.bin.gz` dans les Releases
- Tester le reload avant de mettre en prod
- Monitorer les logs après update automatique
- Garder des secrets sécurisés (pas en hardcoded)

❌ **À ne pas faire** :
- Committer des fichiers binaires volumineux (> 100 MB)
- Ignorer les erreurs de décodage binaire
- Laisser les secrets en dur dans le code
- Déboguer sur la prod (utiliser logs)

---

## Support & Ressources

- [Render Docs](https://render.com/docs)
- [GitHub Actions Secrets](https://docs.github.com/en/actions/security-guides/encrypted-secrets)
- [SNCF Data API](https://ressources.data.sncf.com)
- Issues/Questions : Créer un ticket GitHub

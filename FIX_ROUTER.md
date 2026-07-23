FIX the CI failure in this repo. 

The CI frontend-test job failed with:
```
Cannot find name 'router'. Did you mean 'route'?
```

The issue is in `web-ui/src/views/IssuesView.vue`:
- Line 56: `import { useRoute, useRouter } from 'vue-router'` (I already added useRouter)
- Line 63: `const route = useRoute()`
- Line 91: `router.replace({ path: ... })` — uses `router` but it's never declared

Fix: Add `const router = useRouter()` near line 63 alongside the existing `const route = useRoute()`.

Then verify `npm run build` passes.

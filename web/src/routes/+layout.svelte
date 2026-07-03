<script lang="ts">
  import '../app.css';

  const uiBuildHash = __PHOTOSTORE_UI_BUILD_HASH__;
</script>

<slot />

<div class="build-badge" data-testid="ui-build-hash" title={`UI build ${uiBuildHash}`}>
  UI {uiBuildHash}
</div>

(() => {
  const search = document.querySelector("#artist-search");
  const grid = document.querySelector("#artist-grid");
  const notice = document.querySelector("#notice");
  const count = document.querySelector("#result-count");
  const dialog = document.querySelector("#artist-dialog");
  const dialogContent = document.querySelector("#dialog-content");
  const refreshButton = document.querySelector("#refresh-button");
  let searchTimer;

  function setNotice(message, isError = false) {
    notice.textContent = message;
    notice.classList.toggle("notice-error", isError);
  }

  function renderArtists(artists) {
    count.textContent = String(artists.length);
    if (artists.length === 0) {
      grid.innerHTML = '<div class="empty-state">No artists match that search. Try another name, member, or place.</div>';
      return;
    }

    const fragment = document.createDocumentFragment();
    artists.forEach((artist) => {
      const article = document.createElement("article");
      article.className = "artist-card";
      article.dataset.id = artist.id;
      const button = document.createElement("button");
      button.className = "card-action";
      button.type = "button";
      button.dataset.details = artist.id;
      button.setAttribute("aria-label", `View ${artist.name} details`);

      const imageWrap = document.createElement("span");
      imageWrap.className = "card-image-wrap";
      const image = document.createElement("img");
      image.className = "card-image";
      image.src = artist.image;
      image.alt = artist.name;
      image.loading = "lazy";
      const arrow = document.createElement("span");
      arrow.className = "card-arrow";
      arrow.setAttribute("aria-hidden", "true");
      arrow.textContent = "↗";
      imageWrap.append(image, arrow);

      const info = document.createElement("span");
      info.className = "card-info";
      const kicker = document.createElement("span");
      kicker.className = "card-kicker";
      kicker.textContent = `EST. ${artist.creationDate}`;
      const name = document.createElement("span");
      name.className = "card-name";
      name.textContent = artist.name;
      const meta = document.createElement("span");
      meta.className = "card-meta";
      meta.textContent = `${artist.members.length} members · First album ${artist.firstAlbum}`;
      info.append(kicker, name, meta);
      button.append(imageWrap, info);
      article.append(button);
      fragment.append(article);
    });
    grid.replaceChildren(fragment);
  }

  async function searchArtists() {
    const query = search.value.trim();
    setNotice(query ? "Searching the artist index…" : "");
    try {
      const response = await fetch(`/api/artists?q=${encodeURIComponent(query)}`);
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || "Could not search artists.");
      renderArtists(result);
      setNotice("");
    } catch (error) {
      setNotice(error.message, true);
    }
  }

  function formatLocation(location) {
    return location.replaceAll("_", " ").replaceAll("-", ", ");
  }

  async function showDetails(id) {
    dialogContent.textContent = "Loading artist details…";
    dialog.showModal();
    try {
      const response = await fetch(`/api/artists/${encodeURIComponent(id)}`);
      const artist = await response.json();
      if (!response.ok) throw new Error(artist.error || "Could not load artist details.");
      renderDetails(artist);
    } catch (error) {
      dialogContent.textContent = error.message;
    }
  }

  function renderDetails(artist) {
    const heading = document.createElement("div");
    heading.className = "detail-heading";
    const image = document.createElement("img");
    image.src = artist.image;
    image.alt = artist.name;
    image.className = "detail-image";
    const identity = document.createElement("div");
    const eyebrow = document.createElement("p");
    eyebrow.className = "eyebrow";
    eyebrow.textContent = `ESTABLISHED ${artist.creationDate}`;
    const title = document.createElement("h2");
    title.id = "dialog-title";
    title.textContent = artist.name;
    const album = document.createElement("p");
    album.className = "detail-album";
    album.textContent = `First album · ${artist.firstAlbum}`;
    identity.append(eyebrow, title, album);
    heading.append(image, identity);

    const membersSection = document.createElement("section");
    membersSection.className = "detail-section";
    const membersHeading = document.createElement("h3");
    membersHeading.textContent = `Members (${artist.members.length})`;
    const members = document.createElement("ul");
    members.className = "member-list";
    artist.members.forEach((member) => {
      const item = document.createElement("li");
      item.textContent = member;
      members.append(item);
    });
    membersSection.append(membersHeading, members);

    const concertsSection = document.createElement("section");
    concertsSection.className = "detail-section";
    const concertsHeading = document.createElement("h3");
    concertsHeading.textContent = `Concert locations (${artist.locations.length})`;
    const concerts = document.createElement("ul");
    concerts.className = "concert-list";
    artist.locations.forEach((concert) => {
      const item = document.createElement("li");
      const place = document.createElement("strong");
      place.textContent = formatLocation(concert.location);
      const dates = document.createElement("span");
      dates.textContent = concert.dates.length ? concert.dates.join(" · ") : "Dates to be announced";
      item.append(place, dates);
      concerts.append(item);
    });
    concertsSection.append(concertsHeading, concerts);

    dialogContent.replaceChildren(heading, membersSection, concertsSection);
  }

  search.addEventListener("input", () => {
    window.clearTimeout(searchTimer);
    searchTimer = window.setTimeout(searchArtists, 220);
  });
  grid.addEventListener("click", (event) => {
    const button = event.target.closest("[data-details]");
    if (button) showDetails(button.dataset.details);
  });
  document.querySelector("#dialog-close").addEventListener("click", () => dialog.close());
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });

  refreshButton.addEventListener("click", async () => {
    refreshButton.disabled = true;
    setNotice("Syncing all four datasets…");
    try {
      const response = await fetch("/api/refresh", { method: "POST" });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || "Could not start data sync.");

      const started = Date.now();
      const checkStatus = async () => {
        const statusResponse = await fetch("/api/status");
        const status = await statusResponse.json();
        if (status.refreshing && Date.now() - started < 20000) {
          window.setTimeout(checkStatus, 500);
          return;
        }
        refreshButton.disabled = false;
        if (!status.ready) {
          setNotice("Artist data could not be loaded. Please try syncing again.", true);
          return;
        }
        search.value = "";
        await searchArtists();
        setNotice("All four datasets are up to date.");
      };
      window.setTimeout(checkStatus, 250);
    } catch (error) {
      refreshButton.disabled = false;
      setNotice(error.message, true);
    }
  });
})();

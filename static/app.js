(() => {
  const search = document.querySelector("#artist-search");
  const grid = document.querySelector("#artist-grid");
  const notice = document.querySelector("#notice");
  const count = document.querySelector("#result-count");
  const directoryLabel = document.querySelector("#directory-label");
  const dialog = document.querySelector("#artist-dialog");
  const dialogContent = document.querySelector("#dialog-content");
  const refreshButton = document.querySelector("#refresh-button");
  const filtersForm = document.querySelector("#filters-form");
  const locationOptions = document.querySelector("#location-options");
  const locationSearch = document.querySelector("#location-search");
  const locationCount = document.querySelector("#location-count");
  const filterStatus = document.querySelector("#filter-status");
  const filterInputs = ["#creation-min", "#creation-max", "#album-min", "#album-max", "#members-min", "#members-max"];
  let searchTimer;
  let filterTimer;
  let searchSequence = 0;
  let allLocations = [];

  function setNotice(message, isError = false) {
    notice.textContent = message;
    notice.classList.toggle("notice-error", isError);
  }

  function createArtistCard(artist) {
    const fragment = document.createDocumentFragment();
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
    return fragment;
  }

  function renderArtists(artists) {
    directoryLabel.textContent = "Artists";
    count.textContent = String(artists.length);
    if (artists.length === 0) {
      grid.innerHTML = '<div class="empty-state">No artists match that search. Try another name, member, or place.</div>';
      return;
    }

    const fragment = document.createDocumentFragment();
    artists.forEach((artist) => fragment.append(createArtistCard(artist)));
    grid.className = "artist-grid";
    grid.replaceChildren(fragment);
  }

  function isFilterActive() {
    return filterInputs.some((selector) => document.querySelector(selector).value !== "") ||
      filtersForm.querySelectorAll('input[name="location"]:checked').length > 0;
  }

  function renderLocationOptions() {
    const query = locationSearch.value.trim().toLocaleLowerCase();
    const selected = new Set(
      Array.from(filtersForm.querySelectorAll('input[name="location"]:checked'), (input) => input.value),
    );
    const fragment = document.createDocumentFragment();
    let visibleCount = 0;
    allLocations.forEach((location) => {
      const labelText = formatLocationLabel(location);
      if (query && !`${location} ${labelText}`.toLocaleLowerCase().includes(query)) return;
      const label = document.createElement("label");
      label.className = "location-option";
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.name = "location";
      checkbox.value = location;
      checkbox.checked = selected.has(location);
      checkbox.setAttribute("aria-label", labelText);
      const text = document.createElement("span");
      text.textContent = labelText;
      label.append(checkbox, text);
      fragment.append(label);
      visibleCount += 1;
    });
    locationOptions.replaceChildren(fragment);
    locationCount.textContent = `${visibleCount} places`;
    if (visibleCount === 0) {
      const empty = document.createElement("span");
      empty.className = "filter-subtle";
      empty.textContent = "No locations found.";
      locationOptions.replaceChildren(empty);
    }
  }

  async function loadFilterOptions() {
    try {
      const response = await fetch("/api/filter-options");
      const options = await response.json();
      if (!response.ok) throw new Error(options.error || "Could not load filter options.");
      const previousSelection = new Set(
        Array.from(filtersForm.querySelectorAll('input[name="location"]:checked'), (input) => input.value),
      );
      const bounds = [
        ["#creation-min", options.creationYearMin, options.creationYearMax],
        ["#creation-max", options.creationYearMin, options.creationYearMax],
        ["#album-min", options.firstAlbumYearMin, options.firstAlbumYearMax],
        ["#album-max", options.firstAlbumYearMin, options.firstAlbumYearMax],
        ["#members-min", options.memberCountMin, options.memberCountMax],
        ["#members-max", options.memberCountMin, options.memberCountMax],
      ];
      bounds.forEach(([selector, min, max]) => {
        const input = document.querySelector(selector);
        input.min = String(min);
        input.max = String(max);
        input.placeholder = `${min}–${max}`;
      });
      allLocations = options.locations;
      renderLocationOptions();
      previousSelection.forEach((location) => {
        const checkbox = Array.from(locationOptions.querySelectorAll('input[name="location"]'))
          .find((input) => input.value === location);
        if (checkbox) checkbox.checked = true;
      });
      filterStatus.textContent = "Results update as you change filters.";
    } catch (error) {
      filterStatus.textContent = error.message;
      filterStatus.classList.add("filter-status-error");
    }
  }

  function filterQuery(query) {
    const params = new URLSearchParams();
    params.set("q", query);
    filterInputs.forEach((selector) => {
      const input = document.querySelector(selector);
      if (input.value.trim()) params.set(input.name, input.value.trim());
    });
    filtersForm.querySelectorAll('input[name="location"]:checked').forEach((input) => {
      params.append("location", input.value);
    });
    return params;
  }

  function createSearchMatch(match, label) {
    const button = document.createElement("button");
    button.className = "search-match";
    button.type = "button";
    button.dataset.details = match.artist.id;
    button.setAttribute("aria-label", `View ${match.artist.name}, matching ${label}`);

    const image = document.createElement("img");
    image.src = match.artist.image;
    image.alt = "";
    image.loading = "lazy";

    const copy = document.createElement("span");
    copy.className = "search-match-copy";
    const name = document.createElement("strong");
    name.textContent = match.artist.name;
    const details = document.createElement("span");
    details.textContent = match.matches.map((value) => label === "location" ? formatLocation(value) : value).join(" · ");
    copy.append(name, details);
    button.append(image, copy);
    return button;
  }

  function renderSearchResults(results) {
    const groups = [
      { title: "Artists", label: "artist name", matches: results.artists, cards: true },
      { title: "Members", label: "member", matches: results.members },
      { title: "Concert locations", label: "location", matches: results.locations },
      { title: "Concert dates", label: "date", matches: results.dates },
      { title: "Artist details", label: "artist detail", matches: results.details },
    ];
    const total = groups.reduce((sum, group) => sum + group.matches.length, 0);
    directoryLabel.textContent = "Matches";
    count.textContent = String(total);

    if (total === 0) {
      grid.className = "artist-grid";
      grid.innerHTML = '<div class="empty-state">No matches found. Try an artist, member, location, date, or album.</div>';
      return;
    }

    const fragment = document.createDocumentFragment();
    groups.forEach((group) => {
      if (group.matches.length === 0) return;
      const section = document.createElement("section");
      section.className = "search-group";
      const heading = document.createElement("h3");
      heading.className = "search-group-heading";
      heading.textContent = group.title;
      const groupCount = document.createElement("span");
      groupCount.textContent = String(group.matches.length);
      heading.append(groupCount);

      const items = document.createElement("div");
      items.className = group.cards ? "search-group-cards" : "search-group-list";
      group.matches.forEach((match) => {
        if (group.cards) {
          items.append(createArtistCard(match));
        } else {
          items.append(createSearchMatch(match, group.label));
        }
      });
      section.append(heading, items);
      fragment.append(section);
    });
    grid.className = "search-groups";
    grid.replaceChildren(fragment);
  }

  async function searchArtists() {
    const query = search.value.trim();
    const sequence = ++searchSequence;
    setNotice(query ? "Searching the artist index…" : "");
    if (filterStatus) filterStatus.textContent = "Updating results…";
    try {
      const response = await fetch(`/api/artists?${filterQuery(query)}`);
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || "Could not search artists.");
      if (sequence !== searchSequence) return;
      if (query) renderSearchResults(result);
      else renderArtists(result.artists);
      filterStatus.textContent = `${result.totalArtists} artists match the current filters.`;
      filterStatus.classList.remove("filter-status-error");
      setNotice("");
    } catch (error) {
      if (sequence !== searchSequence) return;
      setNotice(error.message, true);
      filterStatus.textContent = error.message;
      filterStatus.classList.add("filter-status-error");
    }
  }

  function formatLocation(location) {
    return location.replaceAll("_", " ").replaceAll("-", ", ");
  }

  function formatLocationLabel(location) {
    return formatLocation(location).replace(/\b[a-z]/g, (letter) => letter.toUpperCase())
      .replace(/\bUsa\b/g, "USA");
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
  filtersForm.addEventListener("submit", (event) => {
    event.preventDefault();
  });
  filtersForm.addEventListener("input", (event) => {
    if (event.target === locationSearch) {
      renderLocationOptions();
      return;
    }
    if (event.target.type === "checkbox") return;
    filterStatus.textContent = isFilterActive() ? "Updating results…" : "Results update as you change filters.";
    window.clearTimeout(filterTimer);
    filterTimer = window.setTimeout(searchArtists, 220);
  });
  filtersForm.addEventListener("change", (event) => {
    if (event.target.matches('input[name="location"]')) searchArtists();
  });
  document.querySelector("#clear-filters").addEventListener("click", () => {
    filterInputs.forEach((selector) => {
      document.querySelector(selector).value = "";
    });
    filtersForm.querySelectorAll('input[name="location"]:checked').forEach((input) => {
      input.checked = false;
    });
    locationSearch.value = "";
    renderLocationOptions();
    searchArtists();
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
        await loadFilterOptions();
        await searchArtists();
        setNotice("All four datasets are up to date.");
      };
      window.setTimeout(checkStatus, 250);
    } catch (error) {
      refreshButton.disabled = false;
      setNotice(error.message, true);
    }
  });

  loadFilterOptions();
  if (!grid.querySelector(".artist-card")) {
    const waitForInitialCatalog = async () => {
      try {
        const response = await fetch("/api/status");
        const status = await response.json();
        if (status.ready) {
          await loadFilterOptions();
          await searchArtists();
          return;
        }
        if (status.refreshing) {
          window.setTimeout(waitForInitialCatalog, 500);
          return;
        }
        if (status.error) setNotice("Artist data could not be loaded. Please try syncing again.", true);
      } catch (error) {
        setNotice(error.message, true);
      }
    };
    window.setTimeout(waitForInitialCatalog, 500);
  }
})();

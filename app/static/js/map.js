// maps for the home page, group pages and the group form, drawn with maplibre.
// every map sits in a hidden .map-section, so without webgl, or when the map
// style can't load, the page just keeps working without it.
(() => {
	const style = 'https://tiles.openfreemap.org/styles/positron';
	const sfsu = [-122.4799957, 37.7245167];
	const pinColor = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();

	const canvas = document.createElement('canvas');
	if (!window.maplibregl || !(canvas.getContext('webgl2') || canvas.getContext('webgl'))) return;

	const newMap = (el, options) => {
		const section = el.closest('.map-section');
		section.hidden = false;
		const map = new maplibregl.Map({
			container: el, style, center: sfsu, zoom: 11,
			// page scrolling stays page scrolling, zoom with ctrl or two fingers
			cooperativeGestures: true,
			// a short credit in the corner, compact mode opens as a box over small maps
			attributionControl: { compact: false },
			...options,
		});
		map.addControl(new maplibregl.NavigationControl({ showCompass: false }));
		// a tile that fails after the style loaded is fine, but without the style there is no map
		let styled = false;
		map.once('styledata', () => { styled = true; });
		map.on('error', () => {
			if (styled || section.hidden) return;
			section.hidden = true;
			map.remove();
		});
		return map;
	};
	const pinAt = (el) => [Number(el.dataset.lng), Number(el.dataset.lat)];
	// a pin repeats what the page says in text, so tab and screen readers skip it.
	// the tabindex has to be set before setPopup, which only adds one if it's missing
	const newPin = (options) => {
		const marker = new maplibregl.Marker({ color: pinColor, ...options });
		marker.getElement().tabIndex = -1;
		marker.getElement().setAttribute('aria-hidden', 'true');
		return marker;
	};

	// home: a pin for each group card in the results, redrawn when htmx swaps them
	const home = document.getElementById('groups-map');
	if (home) {
		const map = newMap(home);
		// maplibre disables the button itself when location is blocked or missing
		map.addControl(new maplibregl.GeolocateControl());
		const section = home.closest('.map-section');
		const results = document.getElementById('search-results');
		const byCard = new Map();
		let active = null;
		const draw = (animate) => {
			if (section.hidden) return;
			byCard.forEach((m) => m.remove());
			byCard.clear();
			active = null;
			const bounds = new maplibregl.LngLatBounds();
			for (const card of results.querySelectorAll('.card[data-lat]')) {
				const at = pinAt(card);
				const box = document.createElement('div');
				box.append(card.querySelector('a').cloneNode(true));
				const meets = card.querySelector('.meets');
				if (meets) box.append(meets.cloneNode(true));
				byCard.set(card, newPin()
					.setLngLat(at)
					// the list has the same links, so opening a popup leaves focus alone
					.setPopup(new maplibregl.Popup({ maxWidth: '260px', focusAfterOpen: false }).setDOMContent(box))
					.addTo(map));
				bounds.extend(at);
			}
			if (byCard.size) map.fitBounds(bounds, { padding: 60, maxZoom: 14, animate });
		};
		// the pin of the card under the mouse or keyboard focus stands out
		const highlight = (card) => {
			active?.getElement().classList.remove('is-active');
			active = byCard.get(card) ?? null;
			active?.getElement().classList.add('is-active');
		};
		results.addEventListener('mouseover', (e) => highlight(e.target.closest('.card')));
		results.addEventListener('mouseleave', () => highlight(null));
		results.addEventListener('focusin', (e) => highlight(e.target.closest('.card')));
		results.addEventListener('focusout', () => highlight(null));
		draw(false);
		document.body.addEventListener('htmx:afterSwap', (e) => {
			if (e.detail.target === results) draw(true);
		});
	}

	// group page: where this group meets
	const group = document.getElementById('group-map');
	if (group) {
		const map = newMap(group, { center: pinAt(group), zoom: 15 });
		newPin().setLngLat(pinAt(group)).addTo(map);
	}

	// group form: click the map, drag the pin, or pin the center with the keyboard
	const picker = document.getElementById('pin-map');
	if (picker) {
		const lat = document.getElementById('lat');
		const lng = document.getElementById('lng');
		const status = document.getElementById('pin-status');
		const clear = document.getElementById('pin-clear');
		const start = lat.value && lng.value ? [Number(lng.value), Number(lat.value)] : null;
		const map = newMap(picker, start ? { center: start, zoom: 15 } : { zoom: 12 });
		const marker = newPin({ draggable: true });
		// a click on the pin also reaches the map, which would move the pin to the pointer
		marker.getElement().addEventListener('click', (e) => e.stopPropagation());
		const empty = 'Click the map to pin where you meet.';
		const locate = new maplibregl.GeolocateControl();
		map.addControl(locate);
		// the map moves without a word, so say what happened
		locate.on('geolocate', () => { status.textContent = 'The map is centered on you. Pin the map center if you meet here.'; });
		locate.on('error', () => { status.textContent = "Your location isn't available. Click the map instead."; });

		const set = (at) => {
			at = maplibregl.LngLat.convert(at).wrap();
			marker.setLngLat(at).addTo(map);
			lat.value = at.lat.toFixed(6);
			lng.value = at.lng.toFixed(6);
			status.textContent = 'Pinned. Drag the pin or click somewhere else to move it.';
			clear.hidden = false;
		};
		const unset = () => {
			marker.remove();
			lat.value = lng.value = '';
			status.textContent = empty;
			clear.hidden = true;
		};
		if (start) set(start);
		else unset();

		map.on('click', (e) => set(e.lngLat));
		marker.on('dragend', () => set(marker.getLngLat()));
		document.getElementById('pin-center').addEventListener('click', () => set(map.getCenter()));
		clear.addEventListener('click', unset);
	}
})();

// maps for the home page, group pages and the group form, drawn with maplibre.
// every map sits in a hidden .map-section, so without webgl the page just
// keeps working without it.
(() => {
	const style = 'https://tiles.openfreemap.org/styles/positron';
	const sfsu = [-122.4799957, 37.7245167];
	const pinColor = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();

	const canvas = document.createElement('canvas');
	if (!window.maplibregl || !(canvas.getContext('webgl2') || canvas.getContext('webgl'))) return;

	const newMap = (el, options) => {
		el.closest('.map-section').hidden = false;
		const map = new maplibregl.Map({
			container: el, style, center: sfsu, zoom: 11,
			// page scrolling stays page scrolling, zoom with ctrl or two fingers
			cooperativeGestures: true,
			...options,
		});
		map.addControl(new maplibregl.NavigationControl({ showCompass: false }));
		return map;
	};
	const pinAt = (el) => [Number(el.dataset.lng), Number(el.dataset.lat)];

	// home: a pin for each group in the results, redrawn when htmx swaps them
	const home = document.getElementById('groups-map');
	if (home) {
		const map = newMap(home);
		let markers = [];
		const draw = (animate) => {
			markers.forEach((m) => m.remove());
			markers = [];
			const bounds = new maplibregl.LngLatBounds();
			for (const row of document.querySelectorAll('#search-results tr[data-lat]')) {
				const at = pinAt(row);
				const box = document.createElement('div');
				const meets = document.createElement('p');
				meets.append(...row.querySelector('.meets').cloneNode(true).childNodes);
				box.append(row.querySelector('a').cloneNode(true), meets);
				markers.push(new maplibregl.Marker({ color: pinColor })
					.setLngLat(at)
					// the list below has the same links, so opening a popup leaves focus alone
					.setPopup(new maplibregl.Popup({ offset: 30, maxWidth: '260px', focusAfterOpen: false }).setDOMContent(box))
					.addTo(map));
				bounds.extend(at);
			}
			if (markers.length) map.fitBounds(bounds, { padding: 60, maxZoom: 14, animate });
		};
		draw(false);
		document.body.addEventListener('htmx:afterSwap', (e) => {
			if (e.detail.target.id === 'search-results') draw(true);
		});
	}

	// group page: where this group meets
	const group = document.getElementById('group-map');
	if (group) {
		const map = newMap(group, { center: pinAt(group), zoom: 15 });
		new maplibregl.Marker({ color: pinColor }).setLngLat(pinAt(group)).addTo(map);
	}

	// group form: click the map, drag the pin, or pin the center with the keyboard
	const picker = document.getElementById('pin-map');
	if (picker) {
		const lat = document.getElementById('lat');
		const lng = document.getElementById('lng');
		const status = document.getElementById('pin-status');
		const clear = document.getElementById('pin-clear');
		const start = lat.value && lng.value ? [Number(lng.value), Number(lat.value)] : null;
		const map = newMap(picker, start ? { center: start, zoom: 15 } : {});
		const marker = new maplibregl.Marker({ color: pinColor, draggable: true });
		// a click on the pin also reaches the map, which would move the pin to the pointer
		marker.getElement().addEventListener('click', (e) => e.stopPropagation());
		const empty = 'Click the map to pin where you meet.';

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

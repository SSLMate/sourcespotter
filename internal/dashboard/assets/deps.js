(function() {
	let currentController = null;
	const intro = document.getElementById('deps-intro');
	const form = document.getElementById('deps-form');
	const errors = document.getElementById('deps-errors');
	const results = document.getElementById('deps-results');
	const progress = document.getElementById('deps-progress');
	const configDetails = document.getElementById('deps-config');

	function getFormData() {
		return new URLSearchParams(new FormData(form));
	}
	function setFormData(formData) {
		form['package'].value = formData.get('package') ?? '';
		form['platform'].value = formData.get('platform') ?? 'linux/amd64';
		form['tags'].value = formData.get('tags') ?? '';
		form['test'].checked = formData.get('test') === '1';

		configDetails.open =
			form['platform'].value !== 'linux/amd64' ||
			form['tags'].value !== '' ||
			form['test'].checked;
	}
	function loadState() {
		const formData = new URL(location).searchParams;
		setFormData(formData);
		loadResults(formData);
	}
	form.onsubmit = () => {
		const formData = getFormData();
		const url = new URL(location);
		url.search = '?' + formData.toString();
		history.pushState({}, "", url);
		loadResults(formData);
		return false;
	};
	window.addEventListener('popstate', loadState);
	loadState();

	async function loadResults(formData) {
		if (currentController) {
			currentController.abort();
			currentController = null;
		}

		const pkg = formData.get('package') ?? '';
		if (pkg === '') {
			intro.hidden = false;
			errors.hidden = true;
			results.hidden = true;
			progress.hidden = true;
			return;
		}

		intro.hidden = true;
		errors.hidden = true;
		results.hidden = true;
		progress.hidden = false;
		const url = form.action + '?' + formData.toString();
		try {
			currentController = new AbortController();
			const resp = await fetch(url, {signal: currentController.signal});
			if (!resp.ok) {
				const body = await resp.text().catch(() => '');
				const msg = body || `Request to ${url} failed with status ${resp.status} ${resp.statusText}`;
				errors.textContent = msg;
				errors.hidden = false;
				results.hidden = true;
				intro.hidden = true;
				progress.hidden = true;
				return;
			}

			const text = await resp.text();

			// Parse lines: MODULE PACKAGE
			const moduleMap = new Map();
			let mainModule = '';
			const lines = text.split(/\r?\n/);

			for (const line of lines) {
				const trimmed = line.trim();
				if (!trimmed) continue;

				const [depOnly, module, pkg] = trimmed.split(/\s+/, 3);
				if (!module || !pkg) continue;

				if (!moduleMap.has(module)) {
					moduleMap.set(module, []);
				}
				moduleMap.get(module).push(pkg);

				if (depOnly === 'false') {
					mainModule = module;
				}
			}
			moduleMap.delete(mainModule);

			// Build DOM for results
			results.innerHTML = '';
			const h2 = document.createElement('h2');
			if (moduleMap.size===0) {
				h2.textContent = `${mainModule} has no dependencies`;
			} else {
				h2.textContent = `${mainModule} depends on ${moduleMap.size} module${moduleMap.size===1?'':'s'}:`;
			}
			results.appendChild(h2);

			if (moduleMap.size > 0) {
				const resultsUL = document.createElement('ul');
				results.appendChild(resultsUL);

				for (const module of [...moduleMap.keys()].sort()) {
					const packages = moduleMap.get(module);
					const li = document.createElement('li');

					const details = document.createElement('details');
					const summary = document.createElement('summary');

					const codeModule = document.createElement('code');
					codeModule.textContent = module;

					const textNode = document.createTextNode(
						` (${packages.length} package${packages.length === 1 ? '' : 's'})`
					);

					summary.appendChild(codeModule);
					summary.appendChild(textNode);
					details.appendChild(summary);

					const ul = document.createElement('ul');
					for (const p of packages.sort()) {
						const pkgLi = document.createElement('li');
						const codePkg = document.createElement('code');
						codePkg.textContent = p;
						pkgLi.appendChild(codePkg);
						ul.appendChild(pkgLi);
					}

					details.appendChild(ul);
					li.appendChild(details);
					resultsUL.appendChild(li);
				}
			}

			errors.hidden = true;
			results.hidden = false;
			intro.hidden = true;
			progress.hidden = true;
		} catch (err) {
			if (!(err instanceof DOMException && err.name === "AbortError")) {
				errors.textContent = url + ": " + String(err);
				errors.hidden = false;
				results.hidden = true;
				intro.hidden = true;
				progress.hidden = true;
			}
		}
	}
})();

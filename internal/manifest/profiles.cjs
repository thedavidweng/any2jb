"use strict";

// Stage this reader and profiles.json together, without mac2jb's profile copy.
const manifest = require("./profiles.json");
for (const profile of manifest.profiles) {
	Object.freeze(profile.distribution);
	Object.freeze(profile.supportedSmileBASIC);
	Object.freeze(profile);
}
for (const component of manifest.components) {
	Object.freeze(component.distribution);
	Object.freeze(component);
}
Object.freeze(manifest.components);
Object.freeze(manifest.profiles);
Object.freeze(manifest);

function profileById(id) {
	const profile = manifest.profiles.find((profile) => profile.id === id);
	if (!profile) throw new Error(`Unknown host profile: ${id}`);
	return profile;
}

module.exports = { manifest, profileById };

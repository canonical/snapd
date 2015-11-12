# Test gadget snap with pre-installed snaps

1. Branch snappy-systems
2. Modify the `package.yaml` to add a snap, e.g.:

```diff
=== modified file 'generic-amd64/meta/package.yaml'
--- generic-amd64/meta/package.yaml	2015-07-03 12:50:03 +0000
+++ generic-amd64/meta/package.yaml	2015-11-09 16:26:12 +0000
@@ -7,6 +7,8 @@
 config:
     ubuntu-core:
         autopilot: true
+    config-example-bash:
+      msg: "huzzah\n"
 
 oem:
     branding:
@@ -20,3 +22,7 @@
         boot-assets:
             files:
                 - path: grub.cfg
+
+    software:
+      built-in:
+        - config-example-bash.canonical
```

(for amd64, or modify for other arch).

3. Build the gadget snap.
4. Create an image using the gadget snap.
5. Boot the image
6. Run:

        sudo journalctl -u ubuntu-snappy.firstboot.service

    * Check that it shows no errors.


7. Run:

        config-example-bash.hello

    * Check that it prints `huzzah`.

# Test gadget snap with modules

1. Branch snappy-systems
2. Modify the `package.yaml` to add a module, e.g.:

```diff
=== modified file 'generic-amd64/meta/package.yaml'
--- generic-amd64/meta/package.yaml	2015-07-03 12:50:03 +0000
+++ generic-amd64/meta/package.yaml	2015-11-12 10:14:30 +0000
@@ -7,6 +7,7 @@
 config:
     ubuntu-core:
         autopilot: true
+        load-kernel-modules: [tea]
 
 oem:
     branding:

```

3. Build the gadget snap.
4. Create an image using the gadget snap.
5. Boot the image.
6. Run:

        sudo journalctl -u ubuntu-snappy.firstboot.service

    * Check that it shows no errors.


7. Check that the output of `lsmod` includes the module you requested. With the above example,

        lsmod | grep tea



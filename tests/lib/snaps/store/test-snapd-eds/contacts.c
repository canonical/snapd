#include <stdio.h>
#include <string.h>

#include <glib.h>
#include <libebook/libebook.h>

G_DEFINE_AUTOPTR_CLEANUP_FUNC(EBookClient, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(EBookClientCursor, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(ESource, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(ESourceRegistry, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(EContact, g_object_unref)


struct open_data {
    GMainLoop *main_loop;
    const char *source_id;
    GError **error;
    EBookClient **address_book;
    gboolean should_quit;
};

static void
source_added(ESourceRegistry *registry, ESource *source, gpointer user_data)
{
    struct open_data *data = user_data;

    // Ignore sources with the wrong ID
    if (g_strcmp0(e_source_get_uid(source), data->source_id) != 0)
        return;

    *data->address_book = (EBookClient*)e_book_client_connect_sync(
        source, 30, NULL, data->error);

    if (data->should_quit)
        g_main_loop_quit(data->main_loop);
}

static gboolean
source_added_timeout(gpointer user_data)
{
    struct open_data *data = user_data;

    g_set_error_literal(data->error, G_IO_ERROR, G_IO_ERROR_TIMED_OUT,
        "Timed out while waiting for ESource creation from the registry");

    if (data->should_quit)
        g_main_loop_quit(data->main_loop);
    // open_or_create removes us
    return G_SOURCE_CONTINUE;
}

static EBookClient *
open_or_create (ESourceRegistry *registry, const char *source_id,
                GError **error)
{
    g_autoptr(GMainLoop) main_loop = NULL;
    g_autoptr(EBookClient) address_book = NULL;
    g_autoptr(ESource) scratch = NULL;
    g_autoptr(GError) commit_error = NULL;

    main_loop = g_main_loop_new (NULL, FALSE);
    // Listen to registry for added sources
    struct open_data data = {
        .main_loop = main_loop,
        .source_id = source_id,
        .error = error,
        .address_book = &address_book,
        .should_quit = FALSE,
    };
    guint source_added_id = g_signal_connect(registry, "source-added",
                                             G_CALLBACK(source_added), &data);

    // Create a new local address book with the desired source ID
    scratch = e_source_new_with_uid(source_id, NULL, error);
    if (!scratch)
        goto end;
    e_source_set_display_name(scratch, source_id);
    ESourceBackend *backend = e_source_get_extension(
        scratch, E_SOURCE_EXTENSION_ADDRESS_BOOK);
    e_source_backend_set_backend_name(backend, "local");

    // Try to commit the new source to the registry, which will fail
    // if it already exists
    if (!e_source_registry_commit_source_sync(
            registry, scratch, NULL, &commit_error)) {
        if (g_error_matches(commit_error, G_IO_ERROR, G_IO_ERROR_EXISTS)) {
            g_autoptr(ESource) source = e_source_registry_ref_source(
                registry, source_id);
            source_added(registry, source, &data);
        } else {
            g_propagate_error(error, commit_error);
            commit_error = NULL;
            goto end;
        }
    }

    // If we don't have the address book at this point, wait on the
    // source-added signal for it to be created.  Set a timer so we
    // don't wait forever.
    if (!address_book) {
        guint timeout_id = g_timeout_add_seconds(
            20, source_added_timeout, &data);
        data.should_quit = TRUE;
        g_main_loop_run (main_loop);
        g_source_remove(timeout_id);
    }

end:
    if (source_added_id) {
        g_signal_handler_disconnect(registry, source_added_id);
    }
    return g_steal_pointer(&address_book);
}

static gboolean
load_contact_from_stdin(EBookClient *address_book, GError **error)
{
    g_autoptr(GString) vcard_data = g_string_new(NULL);
    char buffer[4092];
    size_t n_read = fread(buffer, 1, sizeof(buffer), stdin);
    while (n_read > 0) {
        g_string_append_len(vcard_data, buffer, n_read);
        n_read = fread(buffer, 1, sizeof(buffer), stdin);
    }

    g_autoptr(EContact) contact = e_contact_new_from_vcard(vcard_data->str);
    if (!contact) {
        g_set_error_literal(error, G_IO_ERROR, G_IO_ERROR_INVALID_DATA,
                            "could not parse vcard");
        return FALSE;
    }

    return e_book_client_add_contact_sync(
        address_book, contact, NULL, NULL, error);
}

static gboolean
list_contacts(EBookClient *address_book, GError **error)
{
    EContactField sort_fields[] = { E_CONTACT_FAMILY_NAME, E_CONTACT_GIVEN_NAME };
    EBookCursorSortType sort_types[] = { E_BOOK_CURSOR_SORT_ASCENDING, E_BOOK_CURSOR_SORT_ASCENDING };
    g_autoptr(EBookClientCursor) cursor = NULL;

    if (!e_book_client_get_cursor_sync(
            address_book, NULL,
            sort_fields, sort_types, G_N_ELEMENTS(sort_fields),
            &cursor, NULL, error)) {
        return FALSE;
    }

    int n_fetched = 0;
    const int chunk_size = 100;
    do {
        GSList *results = NULL;

        n_fetched = e_book_client_cursor_step_sync(
            cursor, E_BOOK_CURSOR_STEP_FETCH | E_BOOK_CURSOR_STEP_MOVE, E_BOOK_CURSOR_ORIGIN_CURRENT,
            chunk_size, &results, NULL, error);
        if (n_fetched < 0) {
            return FALSE;
        }

        const GSList *l;
        for (l = results; l != NULL; l = l->next) {
            EContact *contact = l->data;
            g_autofree char *vcard = e_vcard_to_string (
                E_VCARD(contact), EVC_FORMAT_VCARD_30);

            g_print("%s\n", vcard);
        }
        g_slist_free_full(results, (GDestroyNotify)g_object_unref);
    } while (n_fetched >= chunk_size);

    return TRUE;
}

static gboolean
remove_address_book(EBookClient *address_book, GError **error)
{
    return e_client_remove_sync(E_CLIENT(address_book), NULL, error);
}

int main(int argc, char **argv)
{
    g_autoptr(GError) error = NULL;
    g_autoptr(ESourceRegistry) registry = NULL;
    g_autoptr(EBookClient) address_book = NULL;

    if (argc != 3 || !(!strcmp(argv[1], "load") ||
                       !strcmp(argv[1], "list") ||
                       !strcmp(argv[1], "remove"))) {
        g_printerr("usage: contacts {load|list|remove} ADDRESS-BOOK-ID\n");
        return 1;
    }

    // Connect to the EDS registry service
    registry = e_source_registry_new_sync(NULL, &error);
    if (!registry) {
        goto end;
    }

    address_book = open_or_create(registry, argv[2], &error);
    if (!address_book) {
        goto end;
    }

    if (!strcmp(argv[1], "load")) {
        load_contact_from_stdin(address_book, &error);
    } else if (!strcmp(argv[1], "list")) {
        list_contacts(address_book, &error);
    } else if (!strcmp(argv[1], "remove")) {
        remove_address_book(address_book, &error);
    }

end:
    if (error) {
        g_printerr("error: %s[%d] %s\n", g_quark_to_string(error->domain),
                   error->code, error->message);
        return 1;
    }
    return 0;
}
